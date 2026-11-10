package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

type signInEnv struct {
	run   ingress.Requester[service.PhoneSignIn, service.SignInResult]
	db    *pgxpool.Pool
	clock *restatetest.Clock
}

// startSignIn serves the SignIn service on Postgres stores; each change may
// wrap a store before the service is registered.
func startSignIn(t *testing.T, changes ...func(*service.SignIn)) *signInEnv {
	t.Helper()
	db := pgtest.New(t)
	clock := restatetest.NewClock(now)
	svc := service.SignIn{
		Prefix: restatetest.Prefix(), Accounts: repository.NewAccounts(db), Credentials: repository.NewCredentials(db),
		Now: clock.Now,
	}
	for _, change := range changes {
		change(&svc)
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &signInEnv{run: ingress.Service[service.PhoneSignIn, service.SignInResult](client, svc.ServiceName(), "Run"), db: db, clock: clock}
}

// signIn signs raw in under a fresh key.
func (e *signInEnv) signIn(t *testing.T, raw string) service.SignInResult {
	t.Helper()
	res, err := e.run.Request(context.Background(), service.PhoneSignIn{Phone: phone(t, raw)},
		restate.WithIdempotencyKey(uuid.NewV7().String()))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e *signInEnv) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// refused fails t unless res was refused with code.
func refused(t *testing.T, res service.SignInResult, code string) {
	t.Helper()
	if res.Refused == nil || res.Refused.Code != code {
		t.Fatalf("want %s, got %+v", code, res)
	}
}

func TestSignInCreatesThenFindsTheAccount(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	first := e.signIn(t, "0901234567")
	second := e.signIn(t, "+84 90 123 4567")
	if first.Refused != nil || !first.IsNew || second.Refused != nil || second.IsNew || second.AccountID != first.AccountID ||
		first.SessionID == second.SessionID || e.count(t, "accounts") != 1 || e.count(t, "identities") != 1 {
		t.Fatalf("first %+v, second %+v", first, second)
	}
}

func TestSignInReplayedKey(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	key := uuid.NewV7().String()
	in := service.PhoneSignIn{Phone: phone(t, "0901234567")}
	a, err1 := e.run.Request(context.Background(), in, restate.WithIdempotencyKey(key))
	b, err2 := e.run.Request(context.Background(), in, restate.WithIdempotencyKey(key))
	if err1 != nil || err2 != nil || a.AccountID != b.AccountID || a.SessionID != b.SessionID || e.count(t, "accounts") != 1 {
		t.Fatalf("a %+v %v, b %+v %v", a, err1, b, err2)
	}
}

func TestSignInRefusesASuspendedAccount(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	first := e.signIn(t, "0901234567")
	suspendAccount(t, e.db, first.AccountID)
	refused(t, e.signIn(t, "0901234567"), "account_suspended")
}

func TestSignInMarksTheIdentityUsed(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	first := e.signIn(t, "0901234567")
	e.clock.Add(time.Hour)
	e.signIn(t, "0901234567")
	var used time.Time
	e.db.QueryRow(context.Background(), `SELECT last_used_at FROM identities WHERE account_id = @id`,
		pgx.StrictNamedArgs{"id": first.AccountID}).Scan(&used)
	if !used.Equal(now.Add(time.Hour)) {
		t.Fatalf("last used %v", used)
	}
}

type credentialsCommitThenFail struct {
	repository.Credentials
	restatetest.CommitThenFail
}

func (c *credentialsCommitThenFail) SignUp(ctx context.Context, a domain.Account, cr domain.Credentials) error {
	return c.After(c.Credentials.SignUp(ctx, a, cr))
}

func TestSignInSurvivesCommitThenFail(t *testing.T) {
	t.Parallel()
	e := startSignIn(t, func(s *service.SignIn) { s.Credentials = &credentialsCommitThenFail{Credentials: s.Credentials} })
	res := e.signIn(t, "0901234567")
	if res.Refused != nil || !res.IsNew || e.count(t, "accounts") != 1 || e.count(t, "credentials") != 1 {
		t.Fatalf("%+v, accounts %d, credentials %d", res, e.count(t, "accounts"), e.count(t, "credentials"))
	}
}

// takenCredentials refuses every new identity.
type takenCredentials struct{ repository.Credentials }

func (takenCredentials) SignUp(context.Context, domain.Account, domain.Credentials) error {
	return domain.ErrIdentityTaken
}

func TestSignInRefusesWhenTheIdentityIsTaken(t *testing.T) {
	t.Parallel()
	e := startSignIn(t, func(s *service.SignIn) { s.Credentials = takenCredentials{s.Credentials} })
	refused(t, e.signIn(t, "0901234567"), "try_again")
}

// IDs minted inside Restate are UUIDv7, so ids sort by creation time as the
// spec's cursors assume.
func TestSignInMintsV7IDs(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	first := e.signIn(t, "0901234567")
	e.clock.Add(time.Second)
	second := e.signIn(t, "0901234567")
	for _, id := range []uuid.UUID{first.AccountID, first.SessionID, second.SessionID} {
		if id[6]>>4 != 7 {
			t.Fatalf("%s is version %d", id, id[6]>>4)
		}
	}
	if second.SessionID.Compare(first.SessionID) <= 0 {
		t.Fatal("a later session sorts first")
	}
}

// A sign-in that found the identity before Erase deleted the account gets
// try_again, and the retry signs up a new account.
func TestSignInRacingADeletionTriesAgain(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	first := e.signIn(t, "0901234567")
	if _, err := e.db.Exec(context.Background(), `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": first.AccountID}); err != nil {
		t.Fatal(err)
	}
	refused(t, e.signIn(t, "0901234567"), "try_again")
}
