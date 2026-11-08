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
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// TestParallelRefreshRevokes is the spec's parallel-refresh attack: one token
// used five times at once yields at most one success, and afterwards the
// session no longer refreshes.
func TestParallelRefreshRevokes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	accounts := repository.NewAccounts(db)
	sessions := repository.NewSessions(db)
	acc := seed(t, accounts)
	tokens, err := at(nil, sessions, time.Now()).IssueSession(ctx, uuid.NewV7(), acc, "")
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		tokens domain.Tokens
		err    error
	}
	results := make(chan result, 5)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			out, err := at(accounts, sessions, time.Now()).Refresh(ctx, tokens.RefreshToken)
			results <- result{out, err}
		})
	}
	wg.Wait()
	close(results)
	var winners []domain.Tokens
	for r := range results {
		switch {
		case r.err == nil:
			winners = append(winners, r.tokens)
		case errors.Is(r.err, domain.ErrSessionRevoked):
		default:
			t.Fatalf("unexpected %v", r.err)
		}
	}
	if len(winners) > 1 {
		t.Fatalf("%d refreshes won", len(winners))
	}
	for _, w := range winners {
		if _, err := at(accounts, sessions, time.Now()).Refresh(ctx, w.RefreshToken); !errors.Is(err, domain.ErrSessionRevoked) {
			t.Fatalf("the winner's token after the race: %v", err)
		}
	}
}
