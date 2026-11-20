package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/chat/domain"
)

type pgQuickReplies struct{ db *pgxpool.Pool }

func NewQuickReplies(db *pgxpool.Pool) QuickReplies { return pgQuickReplies{db} }

func (s pgQuickReplies) Get(ctx context.Context, account uuid.UUID) ([]string, error) {
	var items []string
	err := s.db.QueryRow(ctx, `SELECT items FROM quick_replies WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}).Scan(&items)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultQuickReplies(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read quick replies: %w", err)
	}
	return items, nil
}

// Put takes a share lock on the account's row, as every write that keeps a
// row of the caller does, so a PUT racing the account's deletion lands
// before it, and the worker's Delete removes it, or sees the tombstone.
func (s pgQuickReplies) Put(ctx context.Context, account uuid.UUID, items []string) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var deleted bool
		err := tx.QueryRow(ctx, `SELECT status = 'deleted' FROM accounts WHERE id = @id FOR SHARE`,
			pgx.StrictNamedArgs{"id": account}).Scan(&deleted)
		if err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		if deleted {
			return accountdomain.ErrSessionRevoked
		}
		if _, err = tx.Exec(ctx, `INSERT INTO quick_replies (account_id, items) VALUES (@account, @items)
ON CONFLICT (account_id) DO UPDATE SET items = excluded.items`,
			pgx.StrictNamedArgs{"account": account, "items": items}); err != nil {
			return fmt.Errorf("upsert quick replies: %w", err)
		}
		return nil
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
