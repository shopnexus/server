package service_test

import (
	"context"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

type signInEnv struct {
	run        ingress.Requester[service.ProvenSignIn, service.SignInResult]
	db         *pgxpool.Pool
	challenges repository.Challenges
	clock      *restatetest.Clock
}

// startSignIn serves the SignIn service on Postgres stores; each change may
// wrap a store before the service is registered.
func startSignIn(t *testing.T, changes ...func(*service.SignIn)) *signInEnv {
	t.Helper()
	db := pgtest.New(t)
	clock := restatetest.NewClock(now)
	svc := service.SignIn{
		Prefix: restatetest.Prefix(), Accounts: repository.NewAccounts(db), Credentials: repository.NewCredentials(db),
		Challenges: repository.NewChallenges(db), Now: clock.Now,
	}
	for _, change := range changes {
		change(&svc)
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &signInEnv{
		run: ingress.Service[service.ProvenSignIn, service.SignInResult](client, svc.ServiceName(), "Run"),
		db:  db, challenges: repository.NewChallenges(db), clock: clock,
	}
}

// signIn signs raw in under a fresh key.
func (e *signInEnv) signIn(t *testing.T, raw string) service.SignInResult {
	t.Helper()
	res, err := e.run.Request(context.Background(), service.ProvenSignIn{Address: new(phone(t, raw))},
		restate.WithIdempotencyKey(uuid.NewV7().String()))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e *signInEnv) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	q := `SELECT count(*) FROM ` + table
	if table == "accounts" {
		// The ShopNexus system account is in every database; it is not counted.
		q += ` WHERE id <> '` + domain.ShopNexusID.String() + `'`
	}
	if err := e.db.QueryRow(context.Background(), q).Scan(&n); err != nil {
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

// UC-ACC-01 A2.
func TestSignInCreatesThenFindsTheAccount(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	first := e.signIn(t, "0901234567")
	second := e.signIn(t, "+84 90 123 4567")
	if first.Refused != nil || !first.IsNew || second.Refused != nil || second.IsNew || second.AccountID != first.AccountID ||
		e.count(t, "accounts") != 1 || e.count(t, "identities") != 1 {
		t.Fatalf("first %+v, second %+v", first, second)
	}
}

// A retry of the sign-up's key is a new invocation that finds the account
// and answers is_new as the first did; another key's sign-in is not new.
// UC-ACC-01 A2 · NFR-IDEM-01.
func TestSignInRetriedKey(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	in := service.ProvenSignIn{Address: new(phone(t, "0901234567")), Key: uuid.NewV7()}
	a, err1 := e.run.Request(context.Background(), in)
	b, err2 := e.run.Request(context.Background(), in)
	if err1 != nil || err2 != nil || a.AccountID != b.AccountID || !a.IsNew || !b.IsNew || e.count(t, "accounts") != 1 {
		t.Fatalf("a %+v %v, b %+v %v", a, err1, b, err2)
	}
	in.Key = uuid.NewV7()
	if c, err := e.run.Request(context.Background(), in); err != nil || c.AccountID != a.AccountID || c.IsNew {
		t.Fatalf("another key: %+v %v", c, err)
	}
}

// UC-ACC-01 A4 · BR-MOD-02.
func TestSignInLetsASuspendedAccountIn(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	first := e.signIn(t, "0901234567")
	suspendAccount(t, e.db, first.AccountID)
	if again := e.signIn(t, "0901234567"); again.Refused != nil || again.AccountID != first.AccountID || again.IsNew {
		t.Fatalf("%+v", again)
	}
}

// UC-ACC-01.
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

func (c *credentialsCommitThenFail) SignUp(ctx context.Context, a domain.Account, cr domain.Credentials, key uuid.UUID) error {
	return c.After(c.Credentials.SignUp(ctx, a, cr, key))
}

// UC-ACC-01 A2 · NFR-DUR-01.
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

func (takenCredentials) SignUp(context.Context, domain.Account, domain.Credentials, uuid.UUID) error {
	return domain.ErrIdentityTaken
}

// UC-ACC-01 E10 · BR-ACC-05.
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
	if id := first.AccountID; id[6]>>4 != 7 {
		t.Fatalf("%s is version %d", id, id[6]>>4)
	}
}

// A sign-in that found the identity before Erase deleted the account gets
// try_again, and the retry signs up a new account.
// UC-ACC-01 E10.
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

// challenge stores a code challenge for to, returning it and its code.
func (e *signInEnv) challenge(t *testing.T, to string, purpose domain.Purpose, owner *uuid.UUID) (c domain.Challenge, code string) {
	t.Helper()
	id := uuid.NewV7()
	dest := email(t, to)
	code = testCodes.For(id)
	c = domain.NewChallenge(id, purpose, dest, owner, netip.MustParseAddr("127.0.0.1"), testCodes.Hash(dest, code), e.clock.Now())
	if _, err := e.challenges.Create(context.Background(), c, uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	return c, code
}

func (e *signInEnv) guess(t *testing.T, c domain.Challenge, code string) service.SignInResult {
	t.Helper()
	res, err := e.run.Request(context.Background(),
		service.ProvenSignIn{Guess: &service.CodeGuess{ChallengeID: c.ID(), CodeHash: testCodes.Hash(c.Destination(), code)}},
		restate.WithIdempotencyKey(uuid.NewV7().String()))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// wrongCode is a six-digit code other than code.
func wrongCode(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

// UC-ACC-01 A1.
func TestSignInByEmailCode(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	c, code := e.challenge(t, "linh@example.com", domain.PurposeSignIn, nil)
	first := e.guess(t, c, code)
	if first.Refused != nil || !first.IsNew {
		t.Fatalf("%+v", first)
	}
	e.clock.Add(time.Second)
	c2, code2 := e.challenge(t, "linh@example.com", domain.PurposeSignIn, nil)
	second := e.guess(t, c2, code2)
	if second.Refused != nil || second.IsNew || second.AccountID != first.AccountID {
		t.Fatalf("second %+v", second)
	}
	var kind string
	if err := e.db.QueryRow(context.Background(), `SELECT kind FROM identities WHERE account_id = @id`,
		pgx.StrictNamedArgs{"id": first.AccountID}).Scan(&kind); err != nil || kind != "email" {
		t.Fatalf("kind %q %v", kind, err)
	}
}

// UC-ACC-01 E6, E7, E8.
func TestSignInByEmailCodeRefusals(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	c, code := e.challenge(t, "linh@example.com", domain.PurposeSignIn, nil)
	res := e.guess(t, c, wrongCode(code))
	if res.Refused == nil || res.Refused.Code != "invalid_code" {
		t.Fatalf("wrong code: %+v", res)
	}
	for range domain.MaxAttempts - 1 {
		res = e.guess(t, c, wrongCode(code))
	}
	refused(t, res, "code_locked")
	refused(t, e.guess(t, c, code), "code_locked")

	c2, code2 := e.challenge(t, "minh@example.com", domain.PurposeSignIn, nil)
	e.clock.Add(domain.CodeTTL)
	refused(t, e.guess(t, c2, code2), "code_expired")

	// A newer code to the same address replaces the older one.
	old, oldCode := e.challenge(t, "an@example.com", domain.PurposeSignIn, nil)
	e.clock.Add(time.Second)
	e.challenge(t, "an@example.com", domain.PurposeSignIn, nil)
	refused(t, e.guess(t, old, oldCode), "code_expired")
}

// UC-ACC-01 E8.
func TestSignInRefusesAnAddIdentityChallenge(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	owner := e.signIn(t, "0901234567").AccountID
	c, code := e.challenge(t, "linh@example.com", domain.PurposeAddIdentity, &owner)
	refused(t, e.guess(t, c, code), "code_expired")
}

// UC-ACC-01.
func TestSignInByGoogleFindsTheEmailAccount(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	c, code := e.challenge(t, "linh@gmail.com", domain.PurposeSignIn, nil)
	byCode := e.guess(t, c, code)
	res, err := e.run.Request(context.Background(), service.ProvenSignIn{Address: new(email(t, "Linh@Gmail.com"))},
		restate.WithIdempotencyKey(uuid.NewV7().String()))
	if err != nil || res.Refused != nil || res.IsNew || res.AccountID != byCode.AccountID || e.count(t, "accounts") != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}
