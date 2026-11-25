package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgQuickReplies struct{ db *pgxpool.Pool }

func NewQuickReplies(db *pgxpool.Pool) QuickReplies { return pgQuickReplies{db} }

func (s pgQuickReplies) Get(ctx context.Context, account uuid.UUID) (items []string, saved bool, err error) {
	err = s.db.QueryRow(ctx, `SELECT items FROM quick_replies WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}).Scan(&items)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read quick replies: %w", err)
	}
	return items, true, nil
}

// Put takes a share lock on the account's row, as every write that keeps a
// row of the caller does, so a PUT racing the account's deletion lands
// before it, and the worker's Delete removes it, or sees the tombstone.
func (s pgQuickReplies) Put(ctx context.Context, account uuid.UUID, items []string) error {
	b := &pgx.Batch{}
	postgres.QueueLockLiving(b, account, accountdomain.ErrSessionRevoked)
	b.Queue(`INSERT INTO quick_replies (account_id, items) VALUES (@account, @items)
ON CONFLICT (account_id) DO UPDATE SET items = excluded.items`, pgx.StrictNamedArgs{"account": account, "items": items})
	// QueueLockLiving's callback aborts, so the batch needs a transaction.
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("put quick replies: %w", err)
	}
	return nil
}

func (s pgQuickReplies) Delete(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM quick_replies WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("delete quick replies: %w", err)
	}
	return nil
}
