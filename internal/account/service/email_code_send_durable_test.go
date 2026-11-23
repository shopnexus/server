package service_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	emailprovider "github.com/shopnexus/server/internal/provider/email"
)

var testCodes = domain.NewCodes(make([]byte, 32))

// mailbox records mails; failFirst fails the first send once, as a
// provider timing out would.
type mailbox struct {
	mu        sync.Mutex
	sent      []emailprovider.Message
	failFirst bool
	// refuse fails every send as a provider does for an address it will
	// never deliver to.
	refuse bool
	// onFail runs when failFirst fails a send.
	onFail func()
}

func (m *mailbox) Send(_ context.Context, msg emailprovider.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refuse {
		return fmt.Errorf("%w: resend answered 422", emailprovider.ErrRefused)
	}
	if m.failFirst {
		m.failFirst = false
		if m.onFail != nil {
			m.onFail()
		}
		return errors.New("provider timed out")
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mailbox) all() []emailprovider.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]emailprovider.Message(nil), m.sent...)
}

type emailCodeEnv struct {
	db    *pgxpool.Pool
	run   ingress.Requester[service.EmailCodeInput, service.EmailCodeResult]
	mail  *mailbox
	clock *restatetest.Clock
}

func startEmailCodeSend(t *testing.T, mail *mailbox) *emailCodeEnv {
	t.Helper()
	db := pgtest.New(t)
	clock := restatetest.NewClock(now)
	svc := service.EmailCodeSend{
		Prefix: restatetest.Prefix(), Quotas: repository.NewQuotas(db), Challenges: repository.NewChallenges(db),
		Codes: testCodes, Email: mail, Now: clock.Now, Log: discard,
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &emailCodeEnv{
		db:   db,
		run:  ingress.Service[service.EmailCodeInput, service.EmailCodeResult](client, svc.ServiceName(), "Run"),
		mail: mail, clock: clock,
	}
}

func (e *emailCodeEnv) send(t *testing.T, to, ip string) service.EmailCodeResult {
	t.Helper()
	addr, err := contact.ParseEmail(to)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.run.Request(context.Background(),
		service.EmailCodeInput{Email: addr, Purpose: domain.PurposeSignIn, IP: netip.MustParseAddr(ip)},
		restate.WithIdempotencyKey(uuid.NewV7().String()))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestEmailCodeSendMailsTheCode(t *testing.T) {
	t.Parallel()
	e := startEmailCodeSend(t, &mailbox{})
	res := e.send(t, "linh@example.com", "203.0.113.1")
	code := testCodes.For(res.ChallengeID)
	got := e.mail.all()
	if res.Refused != nil || len(got) != 1 || got[0].To != "linh@example.com" || got[0].Key != res.ChallengeID.String() ||
		got[0].Subject != "Mã xác minh ShopNexus: "+code || !res.ExpiresAt.Equal(now.Add(5*time.Minute)) ||
		!res.ResendAt.Equal(now.Add(45*time.Second)) {
		t.Fatalf("%+v, mails %+v", res, got)
	}
}

func TestEmailCodeSendLimits(t *testing.T) {
	t.Parallel()
	e := startEmailCodeSend(t, &mailbox{})
	e.send(t, "linh@example.com", "203.0.113.1")
	if res := e.send(t, "linh@example.com", "203.0.113.1"); res.Refused == nil || res.Refused.Code != "otp_too_soon" ||
		res.Refused.Details.RetryAfter == nil || *res.Refused.Details.RetryAfter != 45 {
		t.Fatalf("resend: %+v", res.Refused)
	}
	for range 4 {
		e.clock.Add(46 * time.Second)
		if res := e.send(t, "linh@example.com", "203.0.113.1"); res.Refused != nil {
			t.Fatalf("send within the hour: %+v", res.Refused)
		}
	}
	e.clock.Add(46 * time.Second)
	if res := e.send(t, "linh@example.com", "203.0.113.1"); res.Refused == nil || res.Refused.Code != "otp_limit" {
		t.Fatalf("sixth send in an hour: %+v", res.Refused)
	}
}

func TestEmailCodeSendLimitsAClient(t *testing.T) {
	t.Parallel()
	e := startEmailCodeSend(t, &mailbox{})
	for n := range 20 {
		if res := e.send(t, fmt.Sprintf("linh%d@example.com", n), "2001:db8::1"); res.Refused != nil {
			t.Fatalf("send %d: %+v", n, res.Refused)
		}
	}
	// Another host of the same /64 shares the quota.
	if res := e.send(t, "minh@example.com", "2001:db8::2"); res.Refused == nil || res.Refused.Code != "otp_limit" {
		t.Fatalf("twenty-first send: %+v", res.Refused)
	}
}

// A send that fails is retried by Restate and mails the same code: the
// code comes from the challenge ID, not from a fresh draw.
func TestEmailCodeSendRetriesTheSameCode(t *testing.T) {
	t.Parallel()
	e := startEmailCodeSend(t, &mailbox{failFirst: true})
	res := e.send(t, "linh@example.com", "203.0.113.1")
	got := e.mail.all()
	if res.Refused != nil || len(got) != 1 || got[0].Subject != "Mã xác minh ShopNexus: "+testCodes.For(res.ChallengeID) {
		t.Fatalf("%+v, mails %+v", res, got)
	}
}

// An address the provider refuses for good ends the send with invalid_email
// instead of a retry that never ends.
func TestEmailCodeSendStopsOnARefusedAddress(t *testing.T) {
	t.Parallel()
	e := startEmailCodeSend(t, &mailbox{refuse: true})
	if res := e.send(t, "linh@example.com", "203.0.113.1"); res.Refused == nil || res.Refused.Code != "invalid_email" {
		t.Fatalf("%+v", res)
	}
}

// A retry that runs after the code expired mails nothing: the code would
// only be refused.
func TestEmailCodeSendSkipsAnExpiredCode(t *testing.T) {
	t.Parallel()
	mail := &mailbox{failFirst: true}
	e := startEmailCodeSend(t, mail)
	mail.onFail = func() { e.clock.Add(domain.CodeTTL) }
	res := e.send(t, "linh@example.com", "203.0.113.1")
	if res.Refused != nil || len(mail.all()) != 0 {
		t.Fatalf("%+v, mails %+v", res, mail.all())
	}
}

// A code that adds an address to an account says so, so nobody reads it out
// believing it signs them in.
func TestEmailCodeSendNamesAnAddIdentityCode(t *testing.T) {
	t.Parallel()
	e := startEmailCodeSend(t, &mailbox{})
	acc := seed(t, repository.NewAccounts(e.db))
	res, err := e.run.Request(context.Background(), service.EmailCodeInput{
		Email: email(t, "linh@example.com"), Purpose: domain.PurposeAddIdentity, AccountID: &acc, IP: netip.MustParseAddr("203.0.113.1"),
	}, restate.WithIdempotencyKey(uuid.NewV7().String()))
	got := e.mail.all()
	if err != nil || res.Refused != nil || len(got) != 1 || !strings.Contains(got[0].Text, "thêm linh@example.com vào một tài khoản") ||
		!strings.Contains(got[0].HTML, "Thêm email vào tài khoản") ||
		strings.HasPrefix(got[0].Subject, "Mã xác minh ShopNexus") {
		t.Fatalf("%+v %v, mails %+v", res, err, got)
	}
}

// A code asked for in English is mailed in English.
func TestEmailCodeSendInEnglish(t *testing.T) {
	t.Parallel()
	e := startEmailCodeSend(t, &mailbox{})
	res, err := e.run.Request(context.Background(), service.EmailCodeInput{
		Email: email(t, "linh@example.com"), Purpose: domain.PurposeSignIn, IP: netip.MustParseAddr("203.0.113.9"), Locale: locale.English,
	}, restate.WithIdempotencyKey(uuid.NewV7().String()))
	got := e.mail.all()
	if err != nil || res.Refused != nil || len(got) != 1 || got[0].Subject != "Your ShopNexus code: "+testCodes.For(res.ChallengeID) ||
		!strings.Contains(got[0].HTML, `lang="en"`) || !strings.Contains(got[0].HTML, "Expires in 5 minutes") {
		t.Fatalf("%+v %v, mails %+v", res, err, got)
	}
}
