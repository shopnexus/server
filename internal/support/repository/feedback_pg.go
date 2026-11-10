package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/support/domain"
)

type pgFeedback struct{ db *pgxpool.Pool }

// NewFeedback returns the Postgres Feedback.
func NewFeedback(db *pgxpool.Pool) Feedback { return pgFeedback{db} }

func (s pgFeedback) Add(ctx context.Context, r Row, key uuid.UUID, since time.Time) (Row, error) {
	out := r
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('feedback:' || @account::text))`,
			pgx.StrictNamedArgs{"account": r.AccountID}); err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		err := tx.QueryRow(ctx, `SELECT id, account_id, body, created_at FROM feedback
WHERE account_id = @account AND request_key = @key`, pgx.StrictNamedArgs{"account": r.AccountID, "key": key}).
			Scan(&out.ID, &out.AccountID, &out.Body, &out.CreatedAt)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find key: %w", err)
		}
		var n int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM feedback WHERE account_id = @account AND created_at > @since`,
			pgx.StrictNamedArgs{"account": r.AccountID, "since": since}).Scan(&n); err != nil {
			return fmt.Errorf("count feedback: %w", err)
		}
		if n >= domain.DailyLimit {
			return domain.ErrFeedbackLimit
		}
		if _, err = tx.Exec(ctx, `INSERT INTO feedback (id, account_id, body, request_key, created_at)
VALUES (@id, @account, @body, @key, @at)`, pgx.StrictNamedArgs{
			"id": r.ID, "account": r.AccountID, "body": r.Body, "key": key, "at": r.CreatedAt,
		}); err != nil {
			return fmt.Errorf("insert feedback: %w", err)
		}
		return nil
	})
	if err != nil {
		return Row{}, fmt.Errorf("add feedback: %w", err)
	}
	return out, nil
}

func (s pgFeedback) List(ctx context.Context, p page.Request) ([]Row, error) {
	rows, err := s.db.Query(ctx, `SELECT id, account_id, body, created_at FROM feedback
WHERE id < @before ORDER BY id DESC LIMIT @limit`, pgx.StrictNamedArgs{"before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select feedback: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Row])
	if err != nil {
		return nil, fmt.Errorf("read feedback: %w", err)
	}
	return out, nil
}
