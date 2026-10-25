package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// TestParallelRefreshRevokes is the spec's parallel-refresh attack: one token
// used five times at once yields at most one success, and afterwards the
// session no longer refreshes.
func TestParallelRefreshRevokes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	accounts := accountsvc.New(accountrepo.NewAccounts(db), time.Now)
	sessions := repository.NewSessions(db)
	acc := uuid.NewV7()
	accounts.Create(ctx, acc, "")
	tokens, err := authWith(sessions, nil, time.Now(), discard).IssueSession(ctx, uuid.NewV7(), acc, "")
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
			out, err := authWith(sessions, accounts, time.Now(), discard).Refresh(ctx, tokens.RefreshToken)
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
		case errors.Is(r.err, domain.ErrRevoked):
		default:
			t.Fatalf("unexpected %v", r.err)
		}
	}
	if len(winners) > 1 {
		t.Fatalf("%d refreshes won", len(winners))
	}
	for _, w := range winners {
		if _, err := authWith(sessions, accounts, time.Now(), discard).Refresh(ctx, w.RefreshToken); !errors.Is(err, domain.ErrRevoked) {
			t.Fatalf("the winner's token after the race: %v", err)
		}
	}
}
