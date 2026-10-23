package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
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
	challenges otp.ChallengeStore // unwrapped, for the test's own reads and writes
	clock      *restatetest.Clock
}

// startSignIn serves the SignIn service on Postgres stores; each change may
// wrap a store before the service is registered.
func startSignIn(t *testing.T, changes ...func(*auth.SignIn)) *signInEnv {
	t.Helper()
	db := pgtest.New(t)
	clock := restatetest.NewClock(now)
	challenges := otp.NewChallengePGStore(db)
	svc := auth.SignIn{
		Prefix: restatetest.Prefix(), Challenges: challenges, Credentials: credentials.NewPGStore(db),
		Accounts: account.Client{Store: account.NewPGStore(db), Now: time.Now}, Now: clock.Now,
	}
	for _, change := range changes {
		change(&svc)
	}
	client := restatetest.Start(t, restate.Reflect(svc))
	return &signInEnv{rs: auth.Restate{Client: client, Prefix: svc.Prefix}, db: db, challenges: challenges, clock: clock}
}

func (e *signInEnv) try(c otp.Challenge, code string) (auth.SignInResult, error) {
	return e.rs.SignIn().Request(context.Background(), auth.SignInInput{ChallengeID: c.ID(), CodeHash: hashOf(c, code)},
		restate.WithIdempotencyKey(uuid.NewV7().String()))
}

func (e *signInEnv) guess(t *testing.T, c otp.Challenge, code string) auth.SignInResult {
	t.Helper()
	res, err := e.try(c, code)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// signIn sends raw a new challenge and answers it with the right code.
func (e *signInEnv) signIn(t *testing.T, raw string) auth.SignInResult {
	t.Helper()
	c, code := stored(t, e.challenges, raw, e.clock.Now())
	return e.guess(t, c, code)
}

func (e *signInEnv) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *signInEnv) attempts(t *testing.T, c otp.Challenge) int {
	t.Helper()
	got, err := e.challenges.Get(context.Background(), c.ID())
	if err != nil {
		t.Fatal(err)
	}
	return got.Attempts()
}

// refused fails t unless res was refused with code.
func refused(t *testing.T, res auth.SignInResult, code string) {
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
	c, code := stored(t, e.challenges, "0901234567", e.clock.Now())
	key := uuid.NewV7().String()
	in := auth.SignInInput{ChallengeID: c.ID(), CodeHash: hashOf(c, code)}
	a, err1 := e.rs.SignIn().Request(context.Background(), in, restate.WithIdempotencyKey(key))
	b, err2 := e.rs.SignIn().Request(context.Background(), in, restate.WithIdempotencyKey(key))
	if err1 != nil || err2 != nil || a.AccountID != b.AccountID || a.SessionID != b.SessionID || e.count(t, "accounts") != 1 {
		t.Fatalf("a %+v %v, b %+v %v", a, err1, b, err2)
	}
}

func TestSignInCodeWorksOnce(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	c, code := stored(t, e.challenges, "0901234567", e.clock.Now())
	if res := e.guess(t, c, code); res.Refused != nil {
		t.Fatalf("first use: %+v", res.Refused)
	}
	refused(t, e.guess(t, c, code), "code_expired")
}

func TestSignInWrongCodeIsCounted(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	c, code := stored(t, e.challenges, "0901234567", e.clock.Now())
	res := e.guess(t, c, wrong(code))
	refused(t, res, "invalid_code")
	if res.Refused.Details["attempts_left"] != float64(otp.MaxAttempts-1) || e.attempts(t, c) != 1 {
		t.Fatalf("details %v, attempts %d", res.Refused.Details, e.attempts(t, c))
	}
}

func TestSignInLocksAfterFiveWrongCodes(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	c, code := stored(t, e.challenges, "0901234567", e.clock.Now())
	for range otp.MaxAttempts - 1 {
		refused(t, e.guess(t, c, wrong(code)), "invalid_code")
	}
	refused(t, e.guess(t, c, wrong(code)), "code_locked")
	refused(t, e.guess(t, c, code), "code_locked")
}

func TestSignInRefusesStaleChallenges(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	old, oldCode := stored(t, e.challenges, "0901234567", e.clock.Now())
	stored(t, e.challenges, "0901234567", e.clock.Now().Add(time.Minute))
	refused(t, e.guess(t, old, oldCode), "code_expired")

	unknown := auth.NewChallenge(codes(), uuid.NewV7(), otp.PurposeLogin, phone(t, "0907654321"), clientIP, e.clock.Now())
	refused(t, e.guess(t, unknown, "123456"), "code_expired")

	c, code := stored(t, e.challenges, "0907654321", e.clock.Now())
	e.clock.Add(otp.CodeTTL)
	refused(t, e.guess(t, c, code), "code_expired")
	if e.attempts(t, c) != 0 {
		t.Fatal("an expired challenge counted the guess")
	}
}

// racingChallenges is a challenge store whose Save always loses.
type racingChallenges struct{ otp.ChallengeStore }

func (racingChallenges) Save(context.Context, *otp.Challenge, uuid.UUID) error {
	return otp.ErrConflict
}

// A reply to a guess that lost a race must not tell a right code from a
// wrong one.
func TestSignInLostRaceSaysTryAgainForAnyCode(t *testing.T) {
	t.Parallel()
	e := startSignIn(t, func(s *auth.SignIn) { s.Challenges = racingChallenges{s.Challenges} })
	c, code := stored(t, e.challenges, "0901234567", e.clock.Now())
	for _, g := range []string{code, wrong(code)} {
		refused(t, e.guess(t, c, g), "try_again")
	}
}

// TestSignInUnderParallelGuesses is the spec's 50-parallel-guesses attack:
// whatever the interleaving, no guess succeeds, at most five are counted, and
// every reply is invalid_code, code_locked or try_again.
func TestSignInUnderParallelGuesses(t *testing.T) {
	t.Parallel()
	e := startSignIn(t)
	c, code := stored(t, e.challenges, "0901234567", e.clock.Now())
	results := make(chan auth.SignInResult, 50)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			res, err := e.try(c, wrong(code))
			if err != nil {
				t.Error(err)
			}
			results <- res
		})
	}
	wg.Wait()
	close(results)
	invalid := 0
	for res := range results {
		switch {
		case res.Refused == nil:
			t.Fatalf("a wrong guess got in: %+v", res)
		case res.Refused.Code == "invalid_code":
			invalid++
		case res.Refused.Code == "code_locked", res.Refused.Code == "try_again":
		default:
			t.Fatalf("unexpected %+v", res.Refused)
		}
	}
	if invalid > otp.MaxAttempts-1 || e.attempts(t, c) > otp.MaxAttempts {
		t.Fatalf("invalid replies %d, attempts %d", invalid, e.attempts(t, c))
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
	auth.AccountPort
	restatetest.CommitThenFail
}

func (a *accountCommitThenFail) Create(ctx context.Context, id uuid.UUID, name string) error {
	return a.After(a.AccountPort.Create(ctx, id, name))
}

func TestSignInSurvivesCommitThenFail(t *testing.T) {
	t.Parallel()
	e := startSignIn(t, func(s *auth.SignIn) { s.Accounts = &accountCommitThenFail{AccountPort: s.Accounts} })
	res := e.signIn(t, "0901234567")
	if res.Refused != nil || !res.IsNew || e.count(t, "accounts") != 1 || e.count(t, "credentials") != 1 {
		t.Fatalf("%+v, accounts %d", res, e.count(t, "accounts"))
	}
}

type verifyCommitThenFail struct {
	otp.ChallengeStore
	restatetest.CommitThenFail
}

func (v *verifyCommitThenFail) Save(ctx context.Context, c *otp.Challenge, w uuid.UUID) error {
	return v.After(v.ChallengeStore.Save(ctx, c, w))
}

// A verify save that committed and then failed runs again. The verdict was
// taken before it, so a right code still signs in and a wrong one is counted
// once.
func TestSignInSurvivesVerifyCommitThenFail(t *testing.T) {
	t.Parallel()
	for _, right := range []bool{true, false} {
		e := startSignIn(t, func(s *auth.SignIn) { s.Challenges = &verifyCommitThenFail{ChallengeStore: s.Challenges} })
		c, code := stored(t, e.challenges, "0901234567", e.clock.Now())
		if !right {
			code = wrong(code)
		}
		res := e.guess(t, c, code)
		if right && (res.Refused != nil || !res.IsNew) {
			t.Fatalf("right code: %+v", res)
		}
		if !right && (res.Refused == nil || res.Refused.Code != "invalid_code" || e.attempts(t, c) != 1) {
			t.Fatalf("wrong code: %+v, attempts %d", res, e.attempts(t, c))
		}
	}
}

// A wrong code on a challenge already used is refused before anything is
// saved, so no retry of that step can mistake it for a counted guess.
func TestSignInWrongCodeOnAUsedChallenge(t *testing.T) {
	t.Parallel()
	e := startSignIn(t, func(s *auth.SignIn) { s.Challenges = &verifyCommitThenFail{ChallengeStore: s.Challenges} })
	c, code := stored(t, e.challenges, "0901234567", e.clock.Now())
	if res := e.guess(t, c, code); res.Refused != nil {
		t.Fatalf("first use: %+v", res.Refused)
	}
	refused(t, e.guess(t, c, wrong(code)), "code_expired")
}

// takenCredentials refuses every new identity.
type takenCredentials struct{ credentials.Store }

func (takenCredentials) Create(context.Context, credentials.Credentials) error {
	return credentials.ErrTaken
}

func TestSignInCompensatesWhenTheIdentityIsTaken(t *testing.T) {
	t.Parallel()
	e := startSignIn(t, func(s *auth.SignIn) { s.Credentials = takenCredentials{s.Credentials} })
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
