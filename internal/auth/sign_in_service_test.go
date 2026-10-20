package auth_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatetest"
)

type signInEnv struct {
	rs         auth.Restate
	db         *pgxpool.Pool
	challenges otp.ChallengeStore
}

// creatingThenFailing commits an account and then reports a retryable error
// once.
type creatingThenFailing struct {
	auth.AccountPort
	failed atomic.Bool
}

func (c *creatingThenFailing) Create(ctx context.Context, id uuid.UUID, name string) error {
	if err := c.AccountPort.Create(ctx, id, name); err != nil {
		return err
	}
	if !c.failed.Swap(true) {
		return errors.New("connection reset after commit")
	}
	return nil
}

// takenCredentials refuses every new identity.
type takenCredentials struct{ credentials.Store }

func (takenCredentials) Create(context.Context, credentials.Credentials) error {
	return credentials.ErrTaken
}

func startSignIn(t *testing.T, wrapAccounts func(auth.AccountPort) auth.AccountPort, wrapCreds func(credentials.Store) credentials.Store) *signInEnv {
	t.Helper()
	db := pgtest.New(t)
	var accounts auth.AccountPort = account.Client{Store: account.NewPGStore(db), Now: time.Now}
	if wrapAccounts != nil {
		accounts = wrapAccounts(accounts)
	}
	var creds credentials.Store = credentials.NewPGStore(db)
	if wrapCreds != nil {
		creds = wrapCreds(creds)
	}
	challenges := otp.NewChallengePGStore(db)
	prefix := restatetest.Prefix()
	client := restatetest.Start(t, restate.Reflect(auth.SignIn{Prefix: prefix, Challenges: challenges, Credentials: creds, Accounts: accounts}))
	return &signInEnv{rs: auth.Restate{Client: client, Prefix: prefix}, db: db, challenges: challenges}
}

func (e *signInEnv) run(t *testing.T, raw, key string) auth.SignInResult {
	t.Helper()
	c, code := stored(t, e.challenges, raw, time.Now())
	res, err := e.rs.SignIn().Request(context.Background(), auth.SignInInput{ChallengeID: c.ID(), CodeHash: hashOf(c, code)},
		restate.WithIdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e *signInEnv) count(t *testing.T, table string) int {
	var n int
	e.db.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n)
	return n
}

func TestSignInCreatesThenFindsTheAccount(t *testing.T) {
	e := startSignIn(t, nil, nil)
	first := e.run(t, "0901234567", uuid.NewV7().String())
	second := e.run(t, "+84 90 123 4567", uuid.NewV7().String())
	if first.Refusal != nil || !first.IsNew || second.Refusal != nil || second.IsNew || second.AccountID != first.AccountID ||
		first.SessionID == second.SessionID || e.count(t, "accounts") != 1 || e.count(t, "identities") != 1 {
		t.Fatalf("first %+v, second %+v", first, second)
	}
}

func TestSignInReplayedKey(t *testing.T) {
	e := startSignIn(t, nil, nil)
	c, code := stored(t, e.challenges, "0901234567", time.Now())
	key := uuid.NewV7().String()
	in := auth.SignInInput{ChallengeID: c.ID(), CodeHash: hashOf(c, code)}
	a, err1 := e.rs.SignIn().Request(context.Background(), in, restate.WithIdempotencyKey(key))
	b, err2 := e.rs.SignIn().Request(context.Background(), in, restate.WithIdempotencyKey(key))
	if err1 != nil || err2 != nil || a.AccountID != b.AccountID || a.SessionID != b.SessionID || e.count(t, "accounts") != 1 {
		t.Fatalf("a %+v %v, b %+v %v", a, err1, b, err2)
	}
}

func TestSignInSurvivesCommitThenFail(t *testing.T) {
	e := startSignIn(t, func(p auth.AccountPort) auth.AccountPort { return &creatingThenFailing{AccountPort: p} }, nil)
	res := e.run(t, "0901234567", uuid.NewV7().String())
	if res.Refusal != nil || !res.IsNew || e.count(t, "accounts") != 1 || e.count(t, "credentials") != 1 {
		t.Fatalf("%+v, accounts %d", res, e.count(t, "accounts"))
	}
}

func TestSignInCompensatesWhenTheIdentityIsTaken(t *testing.T) {
	e := startSignIn(t, nil, func(s credentials.Store) credentials.Store { return takenCredentials{s} })
	res := e.run(t, "0901234567", uuid.NewV7().String())
	if res.Refusal == nil || res.Refusal.Code != "try_again" || e.count(t, "accounts") != 0 {
		t.Fatalf("%+v, accounts %d", res, e.count(t, "accounts"))
	}
}

func TestSignInWrongCodeIsRefused(t *testing.T) {
	e := startSignIn(t, nil, nil)
	c, _ := stored(t, e.challenges, "0901234567", time.Now())
	res, err := e.rs.SignIn().Request(context.Background(), auth.SignInInput{ChallengeID: c.ID(), CodeHash: []byte("nope")},
		restate.WithIdempotencyKey(uuid.NewV7().String()))
	if err != nil || res.Refusal == nil || res.Refusal.Code != "invalid_code" || res.Refusal.Details["attempts_left"] != float64(4) {
		t.Fatalf("%+v %v", res, err)
	}
	if got, _ := e.challenges.Get(context.Background(), c.ID()); got.Attempts() != 1 {
		t.Fatalf("attempts %d", got.Attempts())
	}
}
