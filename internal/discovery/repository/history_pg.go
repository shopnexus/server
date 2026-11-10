package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/discovery/domain"
)

type pgHistory struct{ db *pgxpool.Pool }

// living takes a share lock on account's row and reports whether the
// account still lives. A write that commits after Erase waits for it and
// then sees the tombstone, so discovery's worker, which ran after Erase,
// leaves nothing behind.
func living(ctx context.Context, tx pgx.Tx, account uuid.UUID) (bool, error) {
	var deleted bool
	err := tx.QueryRow(ctx, `SELECT status = 'deleted' FROM accounts WHERE id = @id FOR SHARE`,
		pgx.StrictNamedArgs{"id": account}).Scan(&deleted)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("lock account: %w", err)
	}
	return !deleted, nil
}

func NewHistory(db *pgxpool.Pool) History { return pgHistory{db} }

const rememberSearch = `
INSERT INTO search_history (id, account_id, query_key, query, category, photo_url, searched_at)
VALUES (@id, @account, @key, @query, NULLIF(@category, ''), NULLIF(@photo_url, ''), @at)
ON CONFLICT (account_id, query_key) DO UPDATE
SET id = excluded.id, query = excluded.query, category = excluded.category,
    photo_url = excluded.photo_url, searched_at = excluded.searched_at`

// trimHistory keeps an account's newest domain.MaxHistory entries; the IDs
// are UUIDv7s taken at the write, so they order by it.
const trimHistory = `
DELETE FROM search_history WHERE account_id = @account AND id NOT IN (
    SELECT id FROM search_history WHERE account_id = @account ORDER BY id DESC LIMIT @keep)`

func (s pgHistory) Remember(ctx context.Context, account uuid.UUID, e HistoryEntry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		alive, err := living(ctx, tx, account)
		if err != nil || !alive {
			return err
		}
		if _, err := tx.Exec(ctx, rememberSearch, pgx.StrictNamedArgs{
			"id": uuid.NewV7(), "account": account, "key": domain.HistoryKey(e.Query), "query": e.Query,
			"category": e.Category, "photo_url": e.PhotoURL, "at": e.SearchedAt,
		}); err != nil {
			return fmt.Errorf("upsert search: %w", err)
		}
		if _, err := tx.Exec(ctx, trimHistory, pgx.StrictNamedArgs{"account": account, "keep": domain.MaxHistory}); err != nil {
			return fmt.Errorf("trim history: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("remember search: %w", err)
	}
	return nil
}

func (s pgHistory) List(ctx context.Context, account uuid.UUID) ([]HistoryEntry, error) {
	rows, err := s.db.Query(ctx, `SELECT id, query, coalesce(category, ''), coalesce(photo_url, ''), searched_at
FROM search_history WHERE account_id = @account ORDER BY id DESC`, pgx.StrictNamedArgs{"account": account})
	if err != nil {
		return nil, fmt.Errorf("select history: %w", err)
	}
	entries, err := pgx.CollectRows(rows, pgx.RowToStructByPos[HistoryEntry])
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	return entries, nil
}

func (s pgHistory) Forget(ctx context.Context, account, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM search_history WHERE account_id = @account AND id = @id`,
		pgx.StrictNamedArgs{"account": account, "id": id}); err != nil {
		return fmt.Errorf("forget search: %w", err)
	}
	return nil
}

func (s pgHistory) Clear(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM search_history WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("clear history: %w", err)
	}
	return nil
}
