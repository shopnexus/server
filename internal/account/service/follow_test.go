package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// UC-DSC-04 A2, E1, E2, E3, E5.
func TestFollow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewAccounts(db)
	svc := service.New(service.Deps{
		Accounts: s, Follows: repository.NewFollows(db), Now: func() time.Time { return now }, Log: discard,
	})
	a, b, c := seed(t, s), seed(t, s), seed(t, s)

	if err := svc.Follow(ctx, a, a); !errors.Is(err, domain.ErrSelfFollow) {
		t.Fatalf("self: %v", err)
	}
	if err := svc.Follow(ctx, a, uuid.NewV7()); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	for _, followed := range []uuid.UUID{b, c} {
		if err := svc.Follow(ctx, a, followed); err != nil {
			t.Fatal(err)
		}
	}
	if following, err := svc.Following(ctx, a, b); err != nil || !following {
		t.Fatalf("following: %v %v", following, err)
	}
	if n, err := svc.FollowerCount(ctx, b); err != nil || n != 1 {
		t.Fatalf("count: %d %v", n, err)
	}
	p, err := svc.Follows(ctx, a, page.Request{Limit: 1})
	if err != nil || len(p.Items) != 1 || p.Items[0].AccountID != c || p.Items[0].DisplayName != "Linh" ||
		p.Items[0].At.IsZero() || p.NextCursor == nil {
		t.Fatalf("first page: %+v %v", p, err)
	}
	if p, err = svc.Follows(ctx, a, page.Request{Cursor: *p.NextCursor}); err != nil || len(p.Items) != 1 || p.Items[0].AccountID != b {
		t.Fatalf("second page: %+v %v", p, err)
	}
	followers, err := svc.Followers(ctx, b, page.Request{})
	if err != nil || len(followers.Items) != 1 || followers.Items[0].AccountID != a {
		t.Fatalf("followers: %+v %v", followers, err)
	}

	if err = svc.Block(ctx, c, a); err != nil {
		t.Fatal(err)
	}
	if err = svc.Follow(ctx, a, c); !errors.Is(err, domain.ErrBlocked) {
		t.Fatalf("across a block: %v", err)
	}
	if err = svc.Unfollow(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	if following, _ := svc.Following(ctx, a, b); following {
		t.Fatal("still following")
	}

	suspendAccount(t, db, a)
	if err = svc.Follow(ctx, a, b); !errors.Is(err, domain.ErrSuspended) {
		t.Fatalf("suspended: %v", err)
	}
}

// UC-DSC-04 E6.
func TestFollowADeletedAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	accounts := repository.NewAccounts(db)
	svc := service.New(service.Deps{Accounts: accounts, Follows: repository.NewFollows(db), Now: func() time.Time { return now }, Log: discard})
	me, gone := seed(t, accounts), seed(t, accounts)
	if err := svc.Erase(ctx, gone); err != nil {
		t.Fatal(err)
	}
	if err := svc.Follow(ctx, me, gone); !errors.Is(err, domain.ErrAccountDeleted) {
		t.Fatalf("follow a deleted account: %v", err)
	}
	if err := svc.Follow(ctx, gone, me); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("a deleted account follows: %v", err)
	}
}
