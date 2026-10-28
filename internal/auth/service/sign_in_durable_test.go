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

	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/auth/service"
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
		Prefix: restatetest.Prefix(), Credentials: repository.NewCredentials(db),
		Accounts: accountsvc.New(accountrepo.NewAccounts(db), nil, time.Now), Now: clock.Now,
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

type accountCommitThenFail struct {
	accountsvc.Service
	restatetest.CommitThenFail
}

func (a *accountCommitThenFail) Create(ctx context.Context, id uuid.UUID, name string) error {
	return a.After(a.Service.Create(ctx, id, name))
}

func TestSignInSurvivesCommitThenFail(t *testing.T) {
	t.Parallel()
	e := startSignIn(t, func(s *service.SignIn) { s.Accounts = &accountCommitThenFail{Service: s.Accounts} })
	res := e.signIn(t, "0901234567")
	if res.Refused != nil || !res.IsNew || e.count(t, "accounts") != 1 || e.count(t, "credentials") != 1 {
		t.Fatalf("%+v, accounts %d", res, e.count(t, "accounts"))
	}
}

// takenCredentials refuses every new identity.
type takenCredentials struct{ repository.Credentials }

func (takenCredentials) Create(context.Context, domain.Credentials) error {
	return domain.ErrTaken
}

func TestSignInCompensatesWhenTheIdentityIsTaken(t *testing.T) {
	t.Parallel()
	e := startSignIn(t, func(s *service.SignIn) { s.Credentials = takenCredentials{s.Credentials} })
	res := e.signIn(t, "0901234567")
	refused(t, res, "try_again")
	if e.count(t, "accounts") != 0 {
		t.Fatalf("accounts %d", e.count(t, "accounts"))
	}
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
