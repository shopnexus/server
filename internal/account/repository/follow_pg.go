package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgFollows struct{ db *pgxpool.Pool }

// NewFollows returns the Postgres Follows.
func NewFollows(db *pgxpool.Pool) Follows { return pgFollows{db} }

func (s pgFollows) Follow(ctx context.Context, id, follower, followed uuid.UUID, at time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// The rows' share locks keep a follow from outliving an erasure, the
		// follower's lock keeps the cap and the pair's keeps out a block.
		b := &pgx.Batch{}
		postgres.QueueLockLiving(b, follower, domain.ErrSessionRevoked)
		postgres.QueueLockLiving(b, followed, domain.ErrAccountDeleted)
		postgres.QueueLock(b, "follows", follower)
		queueLockPair(b, follower, followed)
		var (
			blocked, following bool
			n                  int
		)
		b.Queue(`SELECT
    EXISTS (SELECT 1 FROM account_blocks
            WHERE (blocker_id = @a AND blocked_id = @b) OR (blocker_id = @b AND blocked_id = @a)),
    EXISTS (SELECT 1 FROM account_follows WHERE follower_id = @a AND followed_id = @b),
    (SELECT count(*) FROM account_follows WHERE follower_id = @a)`,
			pgx.StrictNamedArgs{"a": follower, "b": followed}).QueryRow(func(row pgx.Row) error {
			if err := row.Scan(&blocked, &following, &n); err != nil {
				return fmt.Errorf("read the follow's facts: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // the queued statements wrap their own errors.
		}
		switch {
		case blocked:
			return domain.ErrBlocked
		case following:
			return nil
		case n >= domain.MaxFollows:
			return domain.ErrFollowLimit
		}
		if _, err := tx.Exec(ctx, `INSERT INTO account_follows (id, follower_id, followed_id, created_at)
VALUES (@id, @follower, @followed, @at)`,
			pgx.StrictNamedArgs{"id": id, "follower": follower, "followed": followed, "at": at}); err != nil {
			return fmt.Errorf("insert follow: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("follow: %w", err)
	}
	return nil
}

func (s pgFollows) Unfollow(ctx context.Context, follower, followed uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM account_follows WHERE follower_id = @follower AND followed_id = @followed`,
		pgx.StrictNamedArgs{"follower": follower, "followed": followed}); err != nil {
		return fmt.Errorf("delete follow: %w", err)
	}
	return nil
}

func (s pgFollows) Following(ctx context.Context, follower, followed uuid.UUID) (bool, error) {
	var following bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM account_follows
WHERE follower_id = @follower AND followed_id = @followed)`,
		pgx.StrictNamedArgs{"follower": follower, "followed": followed}).Scan(&following); err != nil {
		return false, fmt.Errorf("check follow: %w", err)
	}
	return following, nil
}

func (s pgFollows) FollowerCount(ctx context.Context, followed uuid.UUID) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM account_follows WHERE followed_id = @followed`,
		pgx.StrictNamedArgs{"followed": followed}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count followers: %w", err)
	}
	return n, nil
}

func (s pgFollows) ListFollows(ctx context.Context, follower uuid.UUID, p page.Request) ([]Follow, error) {
	return s.query(ctx, `SELECT id, followed_id, created_at FROM account_follows
WHERE follower_id = @account AND id < @before ORDER BY id DESC LIMIT @limit`, follower, p)
}

func (s pgFollows) ListFollowers(ctx context.Context, followed uuid.UUID, p page.Request) ([]Follow, error) {
	return s.query(ctx, `SELECT id, follower_id, created_at FROM account_follows
WHERE followed_id = @account AND id < @before ORDER BY id DESC LIMIT @limit`, followed, p)
}

// query runs one of the two follow queries; ListFollows and
// ListFollowers call it.
func (s pgFollows) query(ctx context.Context, query string, account uuid.UUID, p page.Request) ([]Follow, error) {
	rows, err := s.db.Query(ctx, query, pgx.StrictNamedArgs{"account": account, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select follows: %w", err)
	}
	follows, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Follow])
	if err != nil {
		return nil, fmt.Errorf("read follows: %w", err)
	}
	return follows, nil
}
