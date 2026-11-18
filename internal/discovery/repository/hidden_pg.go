package repository

import (
	"context"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
)

type pgHidden struct{ db *pgxpool.Pool }

func NewHidden(db *pgxpool.Pool) Hidden { return pgHidden{db} }

// trimHidden keeps an account's newest domain.MaxHidden hides; the IDs are
// UUIDv7s taken at the write, so they order by it.
const trimHidden = `
DELETE FROM hidden_listings WHERE account_id = @account AND id NOT IN (
    SELECT id FROM hidden_listings WHERE account_id = @account ORDER BY id DESC LIMIT @keep)`

func (s pgHidden) Hide(ctx context.Context, account, listing uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		alive, err := living(ctx, tx, account)
		if err != nil {
			return err
		}
		if !alive {
			return accountdomain.ErrSessionRevoked
		}
		if _, err := tx.Exec(ctx, `INSERT INTO hidden_listings (id, account_id, listing_id) VALUES (@id, @account, @listing)
ON CONFLICT (account_id, listing_id) DO UPDATE SET id = excluded.id`,
			pgx.StrictNamedArgs{"id": uuid.NewV7(), "account": account, "listing": listing}); err != nil {
			return fmt.Errorf("upsert hide: %w", err)
		}
		if _, err := tx.Exec(ctx, trimHidden, pgx.StrictNamedArgs{"account": account, "keep": domain.MaxHidden}); err != nil {
			return fmt.Errorf("trim hides: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("hide listing: %w", err)
	}
	return nil
}

func (s pgHidden) Unhide(ctx context.Context, account, listing uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM hidden_listings WHERE account_id = @account AND listing_id = @listing`,
		pgx.StrictNamedArgs{"account": account, "listing": listing}); err != nil {
		return fmt.Errorf("unhide listing: %w", err)
	}
	return nil
}

func (s pgHidden) List(ctx context.Context, account uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `SELECT listing_id FROM hidden_listings WHERE account_id = @account ORDER BY id DESC`,
		pgx.StrictNamedArgs{"account": account})
	if err != nil {
		return nil, fmt.Errorf("select hides: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read hides: %w", err)
	}
	return ids, nil
}

func (s pgHidden) Clear(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM hidden_listings WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("clear hides: %w", err)
	}
	return nil
}
