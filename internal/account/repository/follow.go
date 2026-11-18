package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/page"
)

// Follows persists who follows whom. Accounts.Block deletes the follows
// between the two it blocks.
type Follows interface {
	// Follow records that follower follows followed, as id; following again
	// changes nothing. A block between the two, either way, is
	// domain.ErrBlocked, and a follower at domain.MaxFollows is
	// domain.ErrFollowLimit.
	Follow(ctx context.Context, id, follower, followed uuid.UUID, at time.Time) error
	// Unfollow deletes the follow; an absent one is fine.
	Unfollow(ctx context.Context, follower, followed uuid.UUID) error
	// Following reports whether follower follows followed.
	Following(ctx context.Context, follower, followed uuid.UUID) (bool, error)
	// FollowerCount is how many accounts follow followed.
	FollowerCount(ctx context.Context, followed uuid.UUID) (int, error)
	// ListFollows is a query: the accounts follower follows, follow IDs
	// below p.Before(), newest first, at most p.Fetch().
	ListFollows(ctx context.Context, follower uuid.UUID, p page.Request) ([]Follow, error)
	// ListFollowers is a query: the accounts that follow followed, follow
	// IDs below p.Before(), newest first, at most p.Fetch().
	ListFollowers(ctx context.Context, followed uuid.UUID, p page.Request) ([]Follow, error)
}

// Follow is one follow: its ID, the other account and when it started.
type Follow struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	At        time.Time
}
