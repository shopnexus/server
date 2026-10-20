package auth_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatetest"
)

// commitThenFail commits a challenge and then reports a retryable error once,
// as when the process dies before Restate journals the step.
type commitThenFail struct {
	otp.ChallengeStore
	failed atomic.Bool
}

func (c *commitThenFail) Create(ctx context.Context, ch otp.Challenge) error {
	if err := c.ChallengeStore.Create(ctx, ch); err != nil {
		return err
	}
	if !c.failed.Swap(true) {
		return errors.New("connection reset after commit")
	}
	return nil
}

type otpEnv struct {
	rs  auth.Restate
	db  *pgxpool.Pool
	box *inbox
}

// quotaCommitThenFail commits a quota and then reports a retryable error
// once.
type quotaCommitThenFail struct {
	otp.QuotaStore
	failed atomic.Bool
}

func (q *quotaCommitThenFail) Save(ctx context.Context, quota *otp.Quota, w uuid.UUID) error {
	if err := q.QuotaStore.Save(ctx, quota, w); err != nil {
		return err
	}
	if !q.failed.Swap(true) {
		return errors.New("connection reset after commit")
	}
	return nil
}

func startOTP(t *testing.T, wrap func(otp.ChallengeStore) otp.ChallengeStore) *otpEnv {
	return startOTPWith(t, wrap, nil)
}

func startOTPWith(t *testing.T, wrap func(otp.ChallengeStore) otp.ChallengeStore, wrapQuotas func(otp.QuotaStore) otp.QuotaStore) *otpEnv {
	t.Helper()
	db := pgtest.New(t)
	challenges := otp.NewChallengePGStore(db)
	if wrap != nil {
		challenges = wrap(challenges)
	}
	var quotas otp.QuotaStore = otp.NewQuotaPGStore(db)
	if wrapQuotas != nil {
		quotas = wrapQuotas(quotas)
	}
	b := &inbox{}
	prefix := restatetest.Prefix()
	svc := auth.OTP{Prefix: prefix, Quotas: quotas, Challenges: challenges, Codes: codes(), SMS: b, Email: emailInbox{b}}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &otpEnv{rs: auth.Restate{Client: client, Prefix: prefix}, db: db, box: b}
}

func (e *otpEnv) trySend(raw, ip, key string) (auth.OTPSendResult, error) {
	dest, _ := contact.ParsePhone(raw)
	return e.rs.OTP().Request(context.Background(),
		auth.OTPSendInput{Kind: dest.Kind(), Destination: dest.Value(), Purpose: otp.PurposeLogin, IP: netipOf(ip)},
		restate.WithIdempotencyKey(key))
}

func (e *otpEnv) send(t *testing.T, raw, ip, key string) auth.OTPSendResult {
	t.Helper()
	res, err := e.trySend(raw, ip, key)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e *otpEnv) rows(t *testing.T) int {
	var n int
	e.db.QueryRow(context.Background(), `SELECT count(*) FROM otp_challenges`).Scan(&n)
	return n
}

func TestOTPServiceStoresAndSends(t *testing.T) {
	e := startOTP(t, nil)
	res := e.send(t, "0901234567", "203.0.113.7", uuid.NewV7().String())
	msgs := e.box.all()
	if res.Refusal != nil || e.rows(t) != 1 || len(msgs) != 1 || msgs[0].To != "+84901234567" ||
		!strings.Contains(msgs[0].Text, codes().For(res.ChallengeID)) {
		t.Fatalf("result %+v, rows %d, messages %+v", res, e.rows(t), msgs)
	}
}

func TestOTPServiceReplayedKey(t *testing.T) {
	e := startOTP(t, nil)
	key := uuid.NewV7().String()
	a := e.send(t, "0901234567", "203.0.113.7", key)
	b := e.send(t, "0901234567", "203.0.113.7", key)
	if a.ChallengeID != b.ChallengeID || e.rows(t) != 1 || len(e.box.all()) != 1 {
		t.Fatalf("a %+v, b %+v, rows %d, messages %d", a, b, e.rows(t), len(e.box.all()))
	}
}

func TestOTPServiceRefusesTooSoon(t *testing.T) {
	e := startOTP(t, nil)
	e.send(t, "0901234567", "203.0.113.7", uuid.NewV7().String())
	if res := e.send(t, "0901234567", "198.51.100.1", uuid.NewV7().String()); res.Refusal == nil || res.Refusal.Code != "otp_too_soon" {
		t.Fatalf("%+v", res)
	}
}

func TestOTPSendSurvivesCommitThenFail(t *testing.T) {
	e := startOTP(t, func(s otp.ChallengeStore) otp.ChallengeStore { return &commitThenFail{ChallengeStore: s} })
	res := e.send(t, "0901234567", "203.0.113.7", uuid.NewV7().String())
	if res.Refusal != nil || e.rows(t) != 1 || len(e.box.all()) != 1 {
		t.Fatalf("result %+v, rows %d, messages %d", res, e.rows(t), len(e.box.all()))
	}
}

// TestOTPSendCapsOneAddress is the SMS-pumping test: 40 parallel sends from
// one address to 40 numbers let at most 20 codes out.
func TestOTPSendCapsOneAddress(t *testing.T) {
	e := startOTP(t, nil)
	var wg sync.WaitGroup
	var sent, failed atomic.Int32
	for i := range 40 {
		wg.Go(func() {
			res, err := e.trySend(fmt.Sprintf("09%08d", i), "203.0.113.7", uuid.NewV7().String())
			switch {
			case err != nil:
				failed.Add(1)
			case res.Refusal == nil:
				sent.Add(1)
			}
		})
	}
	wg.Wait()
	if failed.Load() > 0 || sent.Load() > 20 || sent.Load() == 0 || len(e.box.all()) != int(sent.Load()) {
		t.Fatalf("failed %d, sent %d, messages %d", failed.Load(), sent.Load(), len(e.box.all()))
	}
}

func TestOTPSendSurvivesQuotaCommitThenFail(t *testing.T) {
	e := startOTPWith(t, nil, func(q otp.QuotaStore) otp.QuotaStore { return &quotaCommitThenFail{QuotaStore: q} })
	res := e.send(t, "0901234567", "203.0.113.7", uuid.NewV7().String())
	if res.Refusal != nil || len(e.box.all()) != 1 {
		t.Fatalf("result %+v, messages %d", res, len(e.box.all()))
	}
	var sends int
	e.db.QueryRow(context.Background(), `SELECT cardinality(sent_at) FROM otp_quotas WHERE key = 'dest:+84901234567'`).Scan(&sends)
	if sends != 1 {
		t.Fatalf("destination quota counted %d sends", sends)
	}
}
