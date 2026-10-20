package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// TestParallelRefreshRevokes is the spec's parallel-refresh attack: one token
// used five times at once yields at most one success, and afterwards the
// session no longer refreshes.
func TestParallelRefreshRevokes(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	accounts := account.Client{Store: account.NewPGStore(db), Now: time.Now}
	sessions, signer := session.NewPGStore(db), session.NewSigner(testKeys)
	acc := uuid.NewV7()
	accounts.Create(ctx, acc, "")
	tokens, err := auth.IssueSession(ctx, sessions, signer, uuid.NewV7(), acc, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		tokens session.Tokens
		err    error
	}
	results := make(chan result, 5)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			out, err := auth.Refresh(ctx, sessions, accounts, signer, tokens.RefreshToken, time.Now(), discard)
			results <- result{out, err}
		})
	}
	wg.Wait()
	close(results)
	var winners []session.Tokens
	for r := range results {
		switch {
		case r.err == nil:
			winners = append(winners, r.tokens)
		case errors.Is(r.err, session.ErrRevoked):
		default:
			t.Fatalf("unexpected %v", r.err)
		}
	}
	if len(winners) > 1 {
		t.Fatalf("%d refreshes won", len(winners))
	}
	for _, w := range winners {
		if _, err := auth.Refresh(ctx, sessions, accounts, signer, w.RefreshToken, time.Now(), discard); !errors.Is(err, session.ErrRevoked) {
			t.Fatalf("the winner's token after the race: %v", err)
		}
	}
}
