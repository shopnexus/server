package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// stores builds the account and follow stores on one fresh database.
func stores(t *testing.T) (*pgxpool.Pool, repository.Accounts, repository.Follows) {
	t.Helper()
	db := pgtest.New(t)
	return db, repository.NewAccounts(db), repository.NewFollows(db)
}

func TestFollows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, s, f := stores(t)
	a, b, c := seed(t, s), seed(t, s), seed(t, s)
	for _, followed := range []uuid.UUID{b, c, b} {
		if err := f.Follow(ctx, uuid.NewV7(), a, followed, now); err != nil {
			t.Fatalf("follow %s: %v", followed, err)
		}
	}
	if err := f.Follow(ctx, uuid.NewV7(), b, a, now); err != nil {
		t.Fatal(err)
	}
	if n, err := f.FollowerCount(ctx, b); err != nil || n != 1 {
		t.Fatalf("b's followers: %d %v", n, err)
	}
	follows, err := f.ListFollows(ctx, a, page.Request{})
	if err != nil || len(follows) != 2 || follows[0].AccountID != c || follows[1].AccountID != b || follows[0].At.IsZero() {
		t.Fatalf("a's follows: %+v %v", follows, err)
	}
	if first, _ := f.ListFollows(ctx, a, page.Request{Limit: 1}); len(first) != 2 {
		t.Fatalf("a page of 1 reads 2 rows: %+v", first)
	}
	followers, err := f.ListFollowers(ctx, a, page.Request{})
	if err != nil || len(followers) != 1 || followers[0].AccountID != b {
		t.Fatalf("a's followers: %+v %v", followers, err)
	}

	// A block, either way, deletes both follows and refuses a new one.
	if err = s.Block(ctx, uuid.NewV7(), b, a, now); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]uuid.UUID{{a, b}, {b, a}} {
		if following, ferr := f.Following(ctx, pair[0], pair[1]); ferr != nil || following {
			t.Fatalf("%v still follows: %v", pair, ferr)
		}
		if ferr := f.Follow(ctx, uuid.NewV7(), pair[0], pair[1], now); !errors.Is(ferr, domain.ErrBlocked) {
			t.Fatalf("%v follows across a block: %v", pair, ferr)
		}
	}
	if following, _ := f.Following(ctx, a, c); !following {
		t.Fatal("the block deleted a follow of someone else")
	}
	for range 2 {
		if err = f.Unfollow(ctx, a, c); err != nil {
			t.Fatal(err)
		}
	}
	if following, _ := f.Following(ctx, a, c); following {
		t.Fatal("still following after unfollow")
	}
}

// fillFollows makes follower follow n new accounts in one statement.
func fillFollows(t *testing.T, db *pgxpool.Pool, follower uuid.UUID, n int) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `WITH made AS (
    INSERT INTO accounts (id, display_name, status, write_id, created_at, updated_at)
    SELECT gen_random_uuid(), 'Linh', 'active', gen_random_uuid(), now(), now() FROM generate_series(1, @n)
    RETURNING id)
INSERT INTO account_follows (id, follower_id, followed_id, created_at)
SELECT gen_random_uuid(), @follower, id, now() FROM made`, pgx.StrictNamedArgs{"n": n, "follower": follower}); err != nil {
		t.Fatal(err)
	}
}

// Two follows at once with one place left: one lands, the other is the cap.
func TestFollowLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, s, f := stores(t)
	a, x, y := seed(t, s), seed(t, s), seed(t, s)
	fillFollows(t, db, a, domain.MaxFollows-1)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, followed := range []uuid.UUID{x, y} {
		wg.Go(func() { errs[i] = f.Follow(ctx, uuid.NewV7(), a, followed, now) })
	}
	wg.Wait()
	ok, capped := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrFollowLimit):
			capped++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || capped != 1 {
		t.Fatalf("%d followed, %d capped", ok, capped)
	}
}

// A follow racing a block between the same two never outlives the block.
func TestFollowRacingABlock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, s, f := stores(t)
	for range 20 {
		a, b := seed(t, s), seed(t, s)
		var wg sync.WaitGroup
		wg.Go(func() { _ = f.Follow(ctx, uuid.NewV7(), a, b, now) })
		wg.Go(func() {
			if err := s.Block(ctx, uuid.NewV7(), b, a, now); err != nil {
				t.Error(err)
			}
		})
		wg.Wait()
		if following, err := f.Following(ctx, a, b); err != nil || following {
			t.Fatalf("a follow outlived the block: %v", err)
		}
	}
}
