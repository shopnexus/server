package service_test

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

var testCodes = domain.NewCodes(make([]byte, 32))

// sentMail is one message a mailbox got.
type sentMail struct{ to, subject, body string }

// mailbox records mails; failFirst fails the first send once, as a
// provider timing out would.
type mailbox struct {
	mu        sync.Mutex
	sent      []sentMail
	failFirst bool
}

func (m *mailbox) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failFirst {
		m.failFirst = false
		return errors.New("provider timed out")
	}
	m.sent = append(m.sent, sentMail{to, subject, body})
	return nil
}

func (m *mailbox) all() []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentMail(nil), m.sent...)
}

type emailCodeEnv struct {
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
		Codes: testCodes, Email: mail, Now: clock.Now,
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &emailCodeEnv{
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
	if res.Refused != nil || len(got) != 1 || got[0].to != "linh@example.com" ||
		got[0].subject != "Mã xác minh ShopNexus: "+code || !res.ExpiresAt.Equal(now.Add(5*time.Minute)) ||
		!res.ResendAt.Equal(now.Add(45*time.Second)) {
		t.Fatalf("%+v, mails %+v", res, got)
	}
}

func TestEmailCodeSendLimits(t *testing.T) {
	t.Parallel()
	e := startEmailCodeSend(t, &mailbox{})
	e.send(t, "linh@example.com", "203.0.113.1")
	if res := e.send(t, "linh@example.com", "203.0.113.1"); res.Refused == nil || res.Refused.Code != "otp_too_soon" ||
		fmt.Sprint(res.Refused.Details["retry_after"]) != "45" {
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
	if res.Refused != nil || len(got) != 1 || got[0].subject != "Mã xác minh ShopNexus: "+testCodes.For(res.ChallengeID) {
		t.Fatalf("%+v, mails %+v", res, got)
	}
}
