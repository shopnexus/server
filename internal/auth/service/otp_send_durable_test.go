package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

type otpEnv struct {
	sender ingress.Requester[service.OTPSendInput, service.OTPSendResult]
	db     *pgxpool.Pool
	box    *inbox
	clock  *restatetest.Clock
}

// startOTP serves the OTP service on Postgres stores; each change may wrap a
// store before the service is registered.
func startOTP(t *testing.T, changes ...func(*service.OTP)) *otpEnv {
	t.Helper()
	db := pgtest.New(t)
	b, clock := &inbox{}, restatetest.NewClock(now)
	svc := service.OTP{
		Prefix: restatetest.Prefix(), Quotas: repository.NewQuotas(db), Challenges: repository.NewChallenges(db),
		Codes: codes(), SMS: b, Email: emailInbox{b}, Now: clock.Now,
	}
	for _, change := range changes {
		change(&svc)
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &otpEnv{sender: ingress.Service[service.OTPSendInput, service.OTPSendResult](client, svc.ServiceName(), "Send"), db: db, box: b, clock: clock}
}

func (e *otpEnv) trySend(raw, ip string) (service.OTPSendResult, error) {
	dest, _ := contact.ParsePhone(raw)
	return e.sender.Request(context.Background(),
		service.OTPSendInput{Kind: dest.Kind(), Destination: dest.Value(), Purpose: domain.PurposeLogin, IP: netipOf(ip)},
		restate.WithIdempotencyKey(uuid.NewV7().String()))
}

func (e *otpEnv) send(t *testing.T, raw, ip string) service.OTPSendResult {
	t.Helper()
	res, err := e.trySend(raw, ip)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// refusedWith fails t unless res was refused with code.
func refusedWith(t *testing.T, res service.OTPSendResult, code string) {
	t.Helper()
	if res.Refused == nil || res.Refused.Code != code {
		t.Fatalf("want %s, got %+v", code, res)
	}
}

func (e *otpEnv) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM otp_challenges`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// sends counts the sends recorded for key; a key with no quota row has none.
func (e *otpEnv) sends(t *testing.T, key string) int {
	t.Helper()
	var n int
	err := e.db.QueryRow(context.Background(), `SELECT cardinality(sent_at) FROM otp_quotas WHERE subject = @subject`,
		pgx.StrictNamedArgs{"subject": key}).Scan(&n)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	return n
}

func TestOTPServiceStoresAndSends(t *testing.T) {
	t.Parallel()
	e := startOTP(t)
	res := e.send(t, "0901234567", "203.0.113.7")
	msgs := e.box.all()
	if res.Refused != nil || e.rows(t) != 1 || len(msgs) != 1 || msgs[0].To != "+84901234567" ||
		!strings.Contains(msgs[0].Text, codes().For(res.ChallengeID)) {
		t.Fatalf("result %+v, rows %d, messages %+v", res, e.rows(t), msgs)
	}
}

func TestOTPServiceReplayedKey(t *testing.T) {
	t.Parallel()
	e := startOTP(t)
	key := uuid.NewV7().String()
	in := service.OTPSendInput{Kind: contact.Phone, Destination: "+84901234567", Purpose: domain.PurposeLogin, IP: clientIP}
	a, err1 := e.sender.Request(context.Background(), in, restate.WithIdempotencyKey(key))
	b, err2 := e.sender.Request(context.Background(), in, restate.WithIdempotencyKey(key))
	if err1 != nil || err2 != nil || a.ChallengeID != b.ChallengeID || e.rows(t) != 1 || len(e.box.all()) != 1 {
		t.Fatalf("a %+v %v, b %+v %v, rows %d, messages %d", a, err1, b, err2, e.rows(t), len(e.box.all()))
	}
}

func TestOTPSendSpacingAndHourlyCap(t *testing.T) {
	t.Parallel()
	e := startOTP(t)
	for i := range 5 {
		if res := e.send(t, "0901234567", "203.0.113.7"); res.Refused != nil {
			t.Fatalf("send %d: %+v", i+1, res.Refused)
		}
		e.clock.Add(44 * time.Second)
		res := e.send(t, "0901234567", "198.51.100.1")
		refusedWith(t, res, "otp_too_soon")
		if res.Refused.Details["retry_after"] != float64(1) {
			t.Fatalf("retry_after %v", res.Refused.Details["retry_after"])
		}
		e.clock.Add(time.Second)
	}
	// The first send leaves the hour 3600 s after it, 225 s from now.
	res := e.send(t, "0901234567", "203.0.113.7")
	refusedWith(t, res, "otp_limit")
	if res.Refused.Details["retry_after"] != float64(3600-225) {
		t.Fatalf("retry_after %v", res.Refused.Details["retry_after"])
	}
}

// TestOTPSendCapsOneAddress is the SMS-pumping test: 40 parallel sends from
// one address to 40 numbers let at most 20 codes out, and another address
// still gets through.
func TestOTPSendCapsOneAddress(t *testing.T) {
	t.Parallel()
	e := startOTP(t)
	var wg sync.WaitGroup
	var sent, failed atomic.Int32
	for i := range 40 {
		wg.Go(func() {
			res, err := e.trySend(fmt.Sprintf("09%08d", i), "203.0.113.7")
			switch {
			case err != nil:
				failed.Add(1)
			case res.Refused == nil:
				sent.Add(1)
			default:
				// Refused by the quota, which the test expects for most sends.
			}
		})
	}
	wg.Wait()
	if failed.Load() > 0 || sent.Load() > 20 || sent.Load() == 0 || len(e.box.all()) != int(sent.Load()) {
		t.Fatalf("failed %d, sent %d, messages %d", failed.Load(), sent.Load(), len(e.box.all()))
	}
	if res := e.send(t, "0987654322", "198.51.100.1"); res.Refused != nil {
		t.Fatalf("another address: %+v", res.Refused)
	}
}

// IPv6 clients are limited per /64, the block one host usually holds.
func TestOTPSendKeysIPv6ByPrefix(t *testing.T) {
	t.Parallel()
	e := startOTP(t)
	for i := range 20 {
		if res := e.send(t, fmt.Sprintf("09%08d", i), fmt.Sprintf("2001:db8:1:2::%x", i+1)); res.Refused != nil {
			t.Fatalf("send %d: %+v", i+1, res.Refused)
		}
	}
	refusedWith(t, e.send(t, "0987654321", "2001:db8:1:2:ffff::1"), "otp_limit")
}

// An address over its limit must not spend the quota of the numbers it
// names, or one machine could lock anyone out of sign-in.
func TestOTPSendChecksTheAddressFirst(t *testing.T) {
	t.Parallel()
	e := startOTP(t)
	for i := range 20 {
		e.send(t, fmt.Sprintf("09%08d", i), "203.0.113.7")
	}
	refusedWith(t, e.send(t, "0987654321", "203.0.113.7"), "otp_limit")
	if res := e.send(t, "0987654321", "198.51.100.9"); res.Refused != nil {
		t.Fatalf("the victim from their own address: %+v", res.Refused)
	}
}

// losing makes the first n quota saves lose a race.
type losing struct {
	repository.Quotas

	n atomic.Int32
}

func (l *losing) Save(ctx context.Context, q *domain.Quota, w uuid.UUID) error {
	if l.n.Add(-1) >= 0 {
		return repository.ErrConflict
	}
	return l.Quotas.Save(ctx, q, w)
}

func TestOTPSendRetriesLostRaces(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		lost    int32
		refused bool
	}{{2, false}, {3, true}} {
		e := startOTP(t, func(s *service.OTP) {
			l := &losing{Quotas: s.Quotas}
			l.n.Store(c.lost)
			s.Quotas = l
		})
		if res := e.send(t, "0901234567", "203.0.113.7"); (res.Refused != nil) != c.refused {
			t.Fatalf("%d lost races: %+v", c.lost, res)
		}
	}
}

type challengeCommitThenFail struct {
	repository.Challenges
	restatetest.CommitThenFail
}

func (c *challengeCommitThenFail) Create(ctx context.Context, ch domain.Challenge) error {
	return c.After(c.Challenges.Create(ctx, ch))
}

func TestOTPSendSurvivesCommitThenFail(t *testing.T) {
	t.Parallel()
	e := startOTP(t, func(s *service.OTP) { s.Challenges = &challengeCommitThenFail{Challenges: s.Challenges} })
	res := e.send(t, "0901234567", "203.0.113.7")
	if res.Refused != nil || e.rows(t) != 1 || len(e.box.all()) != 1 {
		t.Fatalf("result %+v, rows %d, messages %d", res, e.rows(t), len(e.box.all()))
	}
}

type quotaCommitThenFail struct {
	repository.Quotas
	restatetest.CommitThenFail
}

func (q *quotaCommitThenFail) Save(ctx context.Context, quota *domain.Quota, w uuid.UUID) error {
	return q.After(q.Quotas.Save(ctx, quota, w))
}

// A quota save that committed and then failed runs again; the send is still
// counted once in each quota.
func TestOTPSendSurvivesQuotaCommitThenFail(t *testing.T) {
	t.Parallel()
	e := startOTP(t, func(s *service.OTP) { s.Quotas = &quotaCommitThenFail{Quotas: s.Quotas} })
	res := e.send(t, "0901234567", "203.0.113.7")
	if res.Refused != nil || len(e.box.all()) != 1 {
		t.Fatalf("result %+v, messages %d", res, len(e.box.all()))
	}
	if a, d := e.sends(t, "ip:203.0.113.7"), e.sends(t, "dest:+84901234567"); a != 1 || d != 1 {
		t.Fatalf("address quota counted %d sends, destination %d", a, d)
	}
}
