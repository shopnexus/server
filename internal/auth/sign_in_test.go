package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/credentials/credentialstest"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/otp/otptest"
)

// stored creates a login challenge in s and returns it with its code.
func stored(t *testing.T, s otp.ChallengeStore, raw string, at time.Time) (otp.Challenge, string) {
	t.Helper()
	c := auth.NewChallenge(codes(), uuid.NewV7(), otp.PurposeLogin, phone(t, raw), clientIP, at)
	if err := s.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c, codes().For(c.ID())
}

func hashOf(c otp.Challenge, code string) []byte { return codes().Hash(c.Destination(), code) }

// wrong returns a six-digit code that is not code.
func wrong(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}

func TestVerifyChallengeRightCode(t *testing.T) {
	s := otptest.NewChallenges()
	c, code := stored(t, s, "0901234567", now)
	dest, err := auth.VerifyChallenge(context.Background(), s, c.ID(), hashOf(c, code), now, uuid.NewV7())
	if err != nil || dest.Value() != "+84901234567" {
		t.Fatalf("%v %v", dest, err)
	}
	if _, err := auth.VerifyChallenge(context.Background(), s, c.ID(), hashOf(c, code), now, uuid.NewV7()); !errors.Is(err, otp.ErrCodeExpired) {
		t.Fatalf("second use: %v", err)
	}
}

func TestVerifyChallengeWrongCodeIsSaved(t *testing.T) {
	s := otptest.NewChallenges()
	c, code := stored(t, s, "0901234567", now)
	if _, err := auth.VerifyChallenge(context.Background(), s, c.ID(), hashOf(c, wrong(code)), now, uuid.NewV7()); !errors.Is(err, otp.ErrInvalidCode) {
		t.Fatalf("err %v", err)
	}
	if got, _ := s.Get(context.Background(), c.ID()); got.Attempts() != 1 {
		t.Fatalf("attempts %d", got.Attempts())
	}
}

func TestVerifyChallengeRefusals(t *testing.T) {
	s := otptest.NewChallenges()
	old, oldCode := stored(t, s, "0901234567", now)
	stored(t, s, "0901234567", now.Add(time.Minute))
	ctx := context.Background()
	if _, err := auth.VerifyChallenge(ctx, s, old.ID(), hashOf(old, oldCode), now, uuid.NewV7()); !errors.Is(err, otp.ErrCodeExpired) {
		t.Fatalf("replaced: %v", err)
	}
	if _, err := auth.VerifyChallenge(ctx, s, uuid.NewV7(), []byte("x"), now, uuid.NewV7()); !errors.Is(err, otp.ErrCodeExpired) {
		t.Fatalf("unknown: %v", err)
	}
	c, code := stored(t, s, "0907654321", now)
	if _, err := auth.VerifyChallenge(ctx, s, c.ID(), hashOf(c, code), now.Add(otp.CodeTTL), uuid.NewV7()); !errors.Is(err, otp.ErrCodeExpired) {
		t.Fatalf("expired: %v", err)
	}
}

// racing is a challenge store whose Save always loses.
type racing struct{ otp.ChallengeStore }

func (racing) Save(context.Context, *otp.Challenge, uuid.UUID) error { return otp.ErrConflict }

func TestVerifyChallengeConflictSaysTryAgainForAnyCode(t *testing.T) {
	s := otptest.NewChallenges()
	c, code := stored(t, s, "0901234567", now)
	for _, guess := range []string{code, wrong(code)} {
		if _, err := auth.VerifyChallenge(context.Background(), racing{s}, c.ID(), hashOf(c, guess), now, uuid.NewV7()); !errors.Is(err, auth.ErrTryAgain) {
			t.Fatalf("guess %s: %v", guess, err)
		}
	}
}

func TestSignInExisting(t *testing.T) {
	ctx := context.Background()
	accounts, creds := newFakeAccounts(), credentialstest.NewMemStore()
	acc, identID := uuid.NewV7(), uuid.NewV7()
	accounts.Create(ctx, acc, "")
	creds.Create(ctx, credentials.New(acc, credentials.NewIdentity(identID, phone(t, "0901234567"), now), now))

	if err := auth.SignInExisting(ctx, accounts, creds, acc, identID, now.Add(time.Hour), uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if c, _ := creds.Get(ctx, acc); !c.Identities()[0].LastUsedAt().Equal(now.Add(time.Hour)) {
		t.Fatalf("last used %v", c.Identities()[0].LastUsedAt())
	}
	accounts.suspend(acc)
	if err := auth.SignInExisting(ctx, accounts, creds, acc, identID, now, uuid.NewV7()); !errors.Is(err, account.ErrSuspended) {
		t.Fatalf("suspended: %v", err)
	}
}

// A Restate retry runs the step again with the same write ID after the first
// run committed. It must report what the first run decided, not apply the
// guess again.
func TestVerifyChallengeRetryReturnsTheSavedOutcome(t *testing.T) {
	ctx := context.Background()
	s := otptest.NewChallenges()
	c, code := stored(t, s, "0901234567", now)
	w := uuid.NewV7()
	if _, err := auth.VerifyChallenge(ctx, s, c.ID(), hashOf(c, code), now, w); err != nil {
		t.Fatal(err)
	}
	if dest, err := auth.VerifyChallenge(ctx, s, c.ID(), hashOf(c, code), now, w); err != nil || dest.Value() != "+84901234567" {
		t.Fatalf("retry of a right code: %v %v", dest, err)
	}

	bad, badCode := stored(t, s, "0907654321", now)
	w = uuid.NewV7()
	auth.VerifyChallenge(ctx, s, bad.ID(), hashOf(bad, wrong(badCode)), now, w)
	if _, err := auth.VerifyChallenge(ctx, s, bad.ID(), hashOf(bad, wrong(badCode)), now, w); !errors.Is(err, otp.ErrInvalidCode) {
		t.Fatalf("retry of a wrong code: %v", err)
	}
	if got, _ := s.Get(ctx, bad.ID()); got.Attempts() != 1 {
		t.Fatalf("the retry counted the guess again: attempts %d", got.Attempts())
	}
}

// A refused guess that changes nothing must leave no trace a retry could
// mistake for its own write.
func TestVerifyChallengeRetryOfARefusedGuess(t *testing.T) {
	ctx := context.Background()
	s := otptest.NewChallenges()

	expired, code := stored(t, s, "0901234567", now)
	w := uuid.NewV7()
	late := now.Add(otp.CodeTTL)
	for range 2 {
		if _, err := auth.VerifyChallenge(ctx, s, expired.ID(), hashOf(expired, code), late, w); !errors.Is(err, otp.ErrCodeExpired) {
			t.Errorf("expired code, then its retry: %v", err)
		}
	}

	used, code := stored(t, s, "0907654321", now)
	if _, err := auth.VerifyChallenge(ctx, s, used.ID(), hashOf(used, code), now, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	w = uuid.NewV7()
	for range 2 {
		if _, err := auth.VerifyChallenge(ctx, s, used.ID(), hashOf(used, wrong(code)), now, w); !errors.Is(err, otp.ErrCodeExpired) {
			t.Errorf("wrong code on a used challenge, then its retry: %v", err)
		}
	}
}
