package repository

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgHistory struct{ db *pgxpool.Pool }

func NewHistory(db *pgxpool.Pool) History { return pgHistory{db} }

const rememberSearch = `
INSERT INTO search_history (id, account_id, query_key, query, category, category_code, photo_url, searched_at)
VALUES (@id, @account, @key, @query, NULLIF(@category, ''), NULLIF(@category_code, ''), NULLIF(@photo_url, ''), @at)
ON CONFLICT (account_id, query_key) DO UPDATE
SET id = excluded.id, query = excluded.query, category = excluded.category, category_code = excluded.category_code,
    photo_url = excluded.photo_url, searched_at = excluded.searched_at`

// trimHistory keeps an account's newest domain.MaxHistory entries; the IDs
// are UUIDv7s taken at the write, so they order by it.
const trimHistory = `
DELETE FROM search_history WHERE account_id = @account AND id NOT IN (
    SELECT id FROM search_history WHERE account_id = @account ORDER BY id DESC LIMIT @keep)`

func (s pgHistory) Remember(ctx context.Context, account uuid.UUID, e HistoryEntry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		alive, err := postgres.LockLiving(ctx, tx, account)
		if err != nil || !alive {
			return err
		}
		if _, err := tx.Exec(ctx, rememberSearch, pgx.StrictNamedArgs{
			"id": uuid.NewV7(), "account": account, "key": domain.HistoryKey(e.Query), "query": e.Query,
			"category": e.Category, "category_code": e.CategoryCode, "photo_url": e.PhotoURL, "at": e.SearchedAt,
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
	rows, err := s.db.Query(ctx, `SELECT id, query, coalesce(category, ''), coalesce(category_code, ''), coalesce(photo_url, ''), searched_at
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
