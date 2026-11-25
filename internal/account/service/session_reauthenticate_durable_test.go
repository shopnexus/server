package service_test

import (
	"context"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

type reauthEnv struct {
	run         ingress.Requester[service.ReauthenticateRun, service.ReauthenticateResult]
	db          *pgxpool.Pool
	credentials repository.Credentials
	challenges  repository.Challenges
	clock       *restatetest.Clock
}

// startReauthenticate serves the Reauthenticate service on Postgres stores;
// each change may wrap a store before the service is registered.
func startReauthenticate(t *testing.T, changes ...func(*service.Reauthenticate)) *reauthEnv {
	t.Helper()
	db := pgtest.New(t)
	clock := restatetest.NewClock(now)
	svc := service.Reauthenticate{
		Prefix: restatetest.Prefix(), Credentials: repository.NewCredentials(db), Challenges: repository.NewChallenges(db),
		Now: clock.Now,
	}
	for _, change := range changes {
		change(&svc)
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &reauthEnv{
		run: ingress.Service[service.ReauthenticateRun, service.ReauthenticateResult](client, svc.ServiceName(), "Run"),
		db:  db, credentials: repository.NewCredentials(db), challenges: repository.NewChallenges(db), clock: clock,
	}
}

// account signs up with addr through the store.
func (e *reauthEnv) account(t *testing.T, addr contact.Address) uuid.UUID {
	t.Helper()
	a, err := domain.New(uuid.NewV7(), "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.credentials.SignUp(context.Background(), a, domain.NewCredentials(a.ID(), domain.NewIdentity(uuid.NewV7(), addr, now), now), uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	return a.ID()
}

// challenge stores a code challenge for to and returns the guess its right
// code makes.
func (e *reauthEnv) challenge(t *testing.T, to string, purpose domain.Purpose, owner *uuid.UUID) *service.CodeGuess {
	t.Helper()
	id := uuid.NewV7()
	dest := email(t, to)
	hash := testCodes.Hash(dest, testCodes.For(id))
	c := domain.NewChallenge(id, purpose, dest, owner, netip.MustParseAddr("127.0.0.1"), hash, e.clock.Now())
	if _, err := e.challenges.Create(context.Background(), c, uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	return &service.CodeGuess{ChallengeID: id, CodeHash: hash}
}

func (e *reauthEnv) request(t *testing.T, key string, in service.ReauthenticateRun) service.ReauthenticateResult {
	t.Helper()
	res, err := e.run.Request(context.Background(), in, restate.WithIdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e *reauthEnv) reauth(t *testing.T, in service.ReauthenticateRun) service.ReauthenticateResult {
	t.Helper()
	return e.request(t, uuid.NewV7().String(), in)
}

func (e *reauthEnv) identities(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM identities`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// reauthRefused fails t unless res was refused with code.
func reauthRefused(t *testing.T, res service.ReauthenticateResult, code string) {
	t.Helper()
	if res.Refused == nil || res.Refused.Code != code {
		t.Fatalf("want %s, got %+v", code, res)
	}
}

func TestReauthenticateFindsTheCallersIdentity(t *testing.T) {
	t.Parallel()
	e := startReauthenticate(t)
	byPhone := e.account(t, phone(t, "0901234567"))
	byEmail := e.account(t, email(t, "linh@example.com"))
	for name, in := range map[string]service.ReauthenticateRun{
		"phone": {AccountID: byPhone, Address: new(phone(t, "+84 90 123 4567"))},
		"email": {AccountID: byEmail, Address: new(email(t, "Linh@Example.com"))},
	} {
		if res := e.reauth(t, in); res.Refused != nil || !res.At.Equal(now) {
			t.Errorf("%s: %+v", name, res)
		}
	}
}

// A proof of another account and one of an address no account holds get
// the same refusal, and nothing is created.
func TestReauthenticateRefusesAnotherAccountsProof(t *testing.T) {
	t.Parallel()
	e := startReauthenticate(t)
	me := e.account(t, phone(t, "0901234567"))
	e.account(t, phone(t, "0907654321"))
	reauthRefused(t, e.reauth(t, service.ReauthenticateRun{AccountID: me, Address: new(phone(t, "0907654321"))}), "wrong_account")
	reauthRefused(t, e.reauth(t, service.ReauthenticateRun{AccountID: me, Address: new(phone(t, "0911111111"))}), "wrong_account")
	reauthRefused(t, e.reauth(t, service.ReauthenticateRun{AccountID: me, Address: new(email(t, "an@example.com"))}), "wrong_account")
	if n := e.identities(t); n != 2 {
		t.Fatalf("%d identities", n)
	}
}

// A sign-in code to the caller's address works once.
func TestReauthenticateByEmailCode(t *testing.T) {
	t.Parallel()
	e := startReauthenticate(t)
	me := e.account(t, email(t, "linh@example.com"))
	g := e.challenge(t, "linh@example.com", domain.PurposeSignIn, nil)
	if res := e.reauth(t, service.ReauthenticateRun{AccountID: me, Guess: g}); res.Refused != nil || !res.At.Equal(now) {
		t.Fatalf("%+v", res)
	}
	c, err := e.challenges.Get(context.Background(), g.ChallengeID)
	if err != nil || c.ConsumedAt() == nil {
		t.Fatalf("the code is not consumed: %v", err)
	}
	reauthRefused(t, e.reauth(t, service.ReauthenticateRun{AccountID: me, Guess: g}), "code_expired")
}

// Only a sign-in code counts. An add_identity code, even the caller's own,
// is code_expired; a sign-in code to another account's address is spent
// and refused as wrong_account; a wrong code counts a guess.
func TestReauthenticateRefusesACodeOfAnotherPurposeOrAccount(t *testing.T) {
	t.Parallel()
	e := startReauthenticate(t)
	me := e.account(t, email(t, "linh@example.com"))
	e.account(t, email(t, "minh@example.com"))
	own := e.challenge(t, "linh@example.com", domain.PurposeAddIdentity, &me)
	reauthRefused(t, e.reauth(t, service.ReauthenticateRun{AccountID: me, Guess: own}), "code_expired")
	theirs := e.challenge(t, "minh@example.com", domain.PurposeSignIn, nil)
	reauthRefused(t, e.reauth(t, service.ReauthenticateRun{AccountID: me, Guess: theirs}), "wrong_account")
	e.clock.Add(time.Second)
	right := e.challenge(t, "linh@example.com", domain.PurposeSignIn, nil)
	wrong := &service.CodeGuess{ChallengeID: right.ChallengeID, CodeHash: testCodes.Hash(email(t, "linh@example.com"), "000000")}
	if testCodes.For(right.ChallengeID) == "000000" {
		wrong.CodeHash = testCodes.Hash(email(t, "linh@example.com"), "111111")
	}
	res := e.reauth(t, service.ReauthenticateRun{AccountID: me, Guess: wrong})
	if res.Refused == nil || res.Refused.Code != "invalid_code" {
		t.Fatalf("a wrong code: %+v", res)
	}
}

type challengesCommitThenFail struct {
	repository.Challenges
	restatetest.CommitThenFail
}

func (c *challengesCommitThenFail) Save(ctx context.Context, ch *domain.Challenge, w uuid.UUID) error {
	return c.After(c.Challenges.Save(ctx, ch, w))
}

// The counted guess commits and the step then fails; the retried step
// lands the same row, and the code counts once.
func TestReauthenticateSurvivesCommitThenFail(t *testing.T) {
	t.Parallel()
	e := startReauthenticate(t, func(s *service.Reauthenticate) {
		s.Challenges = &challengesCommitThenFail{Challenges: s.Challenges}
	})
	me := e.account(t, email(t, "linh@example.com"))
	g := e.challenge(t, "linh@example.com", domain.PurposeSignIn, nil)
	if res := e.reauth(t, service.ReauthenticateRun{AccountID: me, Guess: g}); res.Refused != nil {
		t.Fatalf("%+v", res)
	}
	c, err := e.challenges.Get(context.Background(), g.ChallengeID)
	if err != nil || c.ConsumedAt() == nil || c.Attempts() != 0 {
		t.Fatalf("consumed %v, attempts %d: %v", c.ConsumedAt(), c.Attempts(), err)
	}
}

// A retry of the request key finds its own guess saved and proves again
// without counting; a wrong code under that key is code_expired.
func TestReauthenticateRetriedKeyProvesAgain(t *testing.T) {
	t.Parallel()
	e := startReauthenticate(t)
	me := e.account(t, email(t, "linh@example.com"))
	g := e.challenge(t, "linh@example.com", domain.PurposeSignIn, nil)
	in := service.ReauthenticateRun{AccountID: me, Guess: g, Key: uuid.NewV7()}
	first, again := e.reauth(t, in), e.reauth(t, in)
	wrong := in
	wrong.Guess = &service.CodeGuess{ChallengeID: g.ChallengeID, CodeHash: []byte("wrong")}
	if first.Refused != nil || again.Refused != nil {
		t.Fatalf("first %+v, again %+v", first, again)
	}
	reauthRefused(t, e.reauth(t, wrong), "code_expired")
	if c, err := e.challenges.Get(context.Background(), g.ChallengeID); err != nil || c.Attempts() != 0 {
		t.Fatalf("attempts %d: %v", c.Attempts(), err)
	}
}
