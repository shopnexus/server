package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// TestVerifyChallengeUnderParallelGuesses is the spec's 50-parallel-guesses
// attack: whatever the interleaving, no guess succeeds, at most five are
// counted, and every reply is invalid_code, code_locked or try_again.
func TestVerifyChallengeUnderParallelGuesses(t *testing.T) {
	ctx := context.Background()
	s := otp.NewChallengePGStore(pgtest.New(t))
	c, code := stored(t, s, "0901234567", now)
	bad := hashOf(c, wrong(code))

	errs := make(chan error, 50)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_, err := auth.VerifyChallenge(ctx, s, c.ID(), bad, now, uuid.NewV7())
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	invalid := 0
	for err := range errs {
		switch {
		case errors.Is(err, otp.ErrInvalidCode):
			invalid++
		case errors.Is(err, otp.ErrCodeLocked), errors.Is(err, apperr.ErrTryAgain):
		default:
			t.Fatalf("unexpected %v", err)
		}
	}
	got, _ := s.Get(ctx, c.ID())
	if invalid > otp.MaxAttempts-1 || got.Attempts() > otp.MaxAttempts || got.ConsumedAt() != nil {
		t.Fatalf("invalid replies %d, attempts %d, consumed %v", invalid, got.Attempts(), got.ConsumedAt())
	}
}
