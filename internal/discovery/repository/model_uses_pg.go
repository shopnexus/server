package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgModelUses struct{ db *pgxpool.Pool }

func NewModelUses(db *pgxpool.Pool) ModelUses { return pgModelUses{db} }

// takeModelUse counts one call on day unless the account made limit calls
// on that day already; a row of an earlier day starts again at one.
const takeModelUse = `
INSERT INTO search_model_uses (account_id, day, calls) VALUES (@account, @day, 1)
ON CONFLICT (account_id) DO UPDATE SET
    calls = CASE WHEN search_model_uses.day = excluded.day THEN search_model_uses.calls + 1 ELSE 1 END,
    day = excluded.day
WHERE search_model_uses.day <> excluded.day OR search_model_uses.calls < @limit
RETURNING calls`

func (s pgModelUses) Take(ctx context.Context, account uuid.UUID, now time.Time, limit int) (bool, error) {
	taken := false
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		alive, err := postgres.LockLiving(ctx, tx, account)
		if err != nil || !alive {
			return err
		}
		var calls int
		err = tx.QueryRow(ctx, takeModelUse, pgx.StrictNamedArgs{
			"account": account, "day": now.UTC().Format(time.DateOnly), "limit": limit,
		}).Scan(&calls)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("count the call: %w", err)
		}
		taken = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("take a model use: %w", err)
	}
	return taken, nil
}

func (s pgModelUses) Clear(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM search_model_uses WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("clear model uses: %w", err)
	}
	return nil
}
