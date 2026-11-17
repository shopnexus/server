package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// stepClock answers first on its first call and then on every later one.
// Account.Reauthenticate reads the clock once before the Reauthenticate
// service reads it, so then is the time the proof is checked.
type stepClock struct {
	mu          sync.Mutex
	called      bool
	first, then time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.called {
		c.called = true
		return c.first
	}
	return c.then
}

// onePhone proves addr whatever the token.
type onePhone struct{ addr contact.Address }

func (p onePhone) Verify(context.Context, string) (contact.Address, error) { return p.addr, nil }

type reauthCase struct {
	svc      *service.Account
	sessions repository.Sessions
	caller   authn.Principal
}

// startReauthCase serves account's Restate services on the clock, signs
// up an account with a phone and starts a session for it that signed in at
// now.
func startReauthCase(t *testing.T, clock func() time.Time) *reauthCase {
	t.Helper()
	db := pgtest.New(t)
	addr := phone(t, "0901234567")
	deps := service.Deps{
		Accounts: repository.NewAccounts(db), Credentials: repository.NewCredentials(db), Sessions: repository.NewSessions(db),
		Challenges: repository.NewChallenges(db), Signer: domain.NewSigner(testKeys), Phones: onePhone{addr}, Google: onePhone{addr},
		Codes: testCodes, Prefix: restatetest.Prefix(), Now: clock, Log: discard,
	}
	deps.Ingress = restatetest.Start(t, service.New(deps).Durable()...)
	a, err := domain.New(uuid.NewV7(), "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = deps.Credentials.SignUp(context.Background(), a, domain.NewCredentials(a.ID(), domain.NewIdentity(uuid.NewV7(), addr, now), now)); err != nil {
		t.Fatal(err)
	}
	sess, _ := domain.NewSession(uuid.NewV7(), a.ID(), "Pixel 9", now, now)
	if _, err = deps.Sessions.Create(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	return &reauthCase{
		svc: service.New(deps), sessions: deps.Sessions,
		caller: authn.Principal{AccountID: a.ID(), SessionID: sess.ID(), AuthTime: now},
	}
}

func (c *reauthCase) reauth() (domain.AccessToken, error) {
	return c.svc.Reauthenticate(context.Background(), service.ReauthenticateInput{
		IdempotencyKey: uuid.NewV7().String(), Caller: c.caller, Proof: service.Proof{PhoneToken: "t"},
	})
}

func (c *reauthCase) authTime(t *testing.T) time.Time {
	t.Helper()
	s, err := c.sessions.Get(context.Background(), c.caller.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	return s.AuthTime()
}

// The session's auth_time becomes the time the proof was checked, and the
// access token carries it.
func TestReauthenticateRenewsTheSession(t *testing.T) {
	t.Parallel()
	checked := now.Add(20 * time.Minute)
	c := startReauthCase(t, (&stepClock{first: checked.Add(-time.Second), then: checked}).Now)
	out, err := c.reauth()
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.NewVerifier(testKeys).Verify(out.AccessToken, checked)
	if err != nil || p.SessionID != c.caller.SessionID || !p.AuthTime.Equal(checked) || !c.authTime(t).Equal(checked) {
		t.Fatalf("%+v %v, row %v", p, err, c.authTime(t))
	}
}

// A session usable when the call began that is past its idle limit when
// the proof is checked gets session_expired, and its auth_time stays.
func TestReauthenticateRefusesASessionThatEndsMeanwhile(t *testing.T) {
	t.Parallel()
	c := startReauthCase(t, (&stepClock{first: now.Add(time.Hour), then: now.Add(domain.SessionIdleLimit)}).Now)
	if _, err := c.reauth(); !errors.Is(err, domain.ErrSessionExpired) {
		t.Fatalf("%v", err)
	}
	if got := c.authTime(t); !got.Equal(now) {
		t.Fatalf("auth_time %v", got)
	}
}
