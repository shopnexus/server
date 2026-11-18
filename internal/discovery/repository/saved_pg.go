package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
)

type pgSaves struct{ db *pgxpool.Pool }

func NewSaves(db *pgxpool.Pool) Saves { return pgSaves{db} }

func (s pgSaves) Save(ctx context.Context, id, account, listing uuid.UUID, price int64, at time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		alive, err := living(ctx, tx, account)
		if err != nil {
			return err
		}
		if !alive {
			return accountdomain.ErrSessionRevoked
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(@account::text))`,
			pgx.StrictNamedArgs{"account": account}); err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		var (
			saved bool
			n     int
		)
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM saved_listings WHERE account_id = @account AND listing_id = @listing),
       (SELECT count(*) FROM saved_listings WHERE account_id = @account)`,
			pgx.StrictNamedArgs{"account": account, "listing": listing}).Scan(&saved, &n); err != nil {
			return fmt.Errorf("count saves: %w", err)
		}
		if saved {
			return nil
		}
		if n >= domain.MaxSaved {
			return domain.ErrLimit
		}
		if _, err := tx.Exec(ctx, `INSERT INTO saved_listings (id, account_id, listing_id, saved_price, created_at)
VALUES (@id, @account, @listing, @price, @at)`, pgx.StrictNamedArgs{
			"id": id, "account": account, "listing": listing, "price": price, "at": at,
		}); err != nil {
			return fmt.Errorf("insert save: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save listing: %w", err)
	}
	return nil
}

func (s pgSaves) Remove(ctx context.Context, account, listing uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM saved_listings WHERE account_id = @account AND listing_id = @listing`,
		pgx.StrictNamedArgs{"account": account, "listing": listing}); err != nil {
		return fmt.Errorf("remove save: %w", err)
	}
	return nil
}

func (s pgSaves) List(ctx context.Context, account uuid.UUID, p page.Request) ([]Save, error) {
	rows, err := s.db.Query(ctx, `SELECT id, listing_id, saved_price, created_at, alert_below, account_id FROM saved_listings
WHERE account_id = @account AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"account": account, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select saves: %w", err)
	}
	saves, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Save])
	if err != nil {
		return nil, fmt.Errorf("read saves: %w", err)
	}
	return saves, nil
}

func (s pgSaves) SavedAmong(ctx context.Context, account uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := s.db.Query(ctx, `SELECT listing_id FROM saved_listings WHERE account_id = @account AND listing_id = ANY(@ids)`,
		pgx.StrictNamedArgs{"account": account, "ids": ids})
	if err != nil {
		return nil, fmt.Errorf("read saves: %w", err)
	}
	saved, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read saves: %w", err)
	}
	out := make(map[uuid.UUID]bool, len(saved))
	for _, id := range saved {
		out[id] = true
	}
	return out, nil
}

func (s pgSaves) Clear(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM saved_listings WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("clear saves: %w", err)
	}
	return nil
}

func (s pgSaves) Get(ctx context.Context, id uuid.UUID) (Save, bool, error) {
	rows, err := s.db.Query(ctx, `SELECT id, listing_id, saved_price, created_at, alert_below, account_id
FROM saved_listings WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return Save{}, false, fmt.Errorf("select save: %w", err)
	}
	save, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Save])
	if errors.Is(err, pgx.ErrNoRows) {
		return Save{}, false, nil
	}
	if err != nil {
		return Save{}, false, fmt.Errorf("read save: %w", err)
	}
	return save, true, nil
}

func (s pgSaves) SetAlert(ctx context.Context, account, listing uuid.UUID, below *int64) (bool, error) {
	tag, err := s.db.Exec(ctx, `UPDATE saved_listings SET alert_below = @below, alerted_price = NULL
WHERE account_id = @account AND listing_id = @listing`,
		pgx.StrictNamedArgs{"account": account, "listing": listing, "below": below})
	if err != nil {
		return false, fmt.Errorf("set alert: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// alertLevel is domain.AlertLevel on saved_listings s.
const alertLevel = `COALESCE(s.alert_below, s.saved_price)`

func (s pgSaves) PriceMoved(ctx context.Context, listing uuid.UUID, price int64, at time.Time) (int, error) {
	var told []uuid.UUID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		args := pgx.StrictNamedArgs{"listing": listing, "price": price}
		if _, err := tx.Exec(ctx, `UPDATE saved_listings s SET alerted_price = NULL
WHERE s.listing_id = @listing AND s.alerted_price IS NOT NULL
  AND `+alertLevel+` <= @price`, args); err != nil {
			return fmt.Errorf("reset alerts: %w", err)
		}
		rows, err := tx.Query(ctx, `UPDATE saved_listings s SET alerted_price = @price
WHERE s.listing_id = @listing AND `+alertLevel+` > @price
  AND (s.alerted_price IS NULL OR s.alerted_price > @price)
RETURNING s.id`, args)
		if err != nil {
			return fmt.Errorf("tell alerts: %w", err)
		}
		if told, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return fmt.Errorf("read alerts: %w", err)
		}
		evs := make([]events.Event, len(told))
		for i, id := range told {
			evs[i] = events.Event{
				ID: uuid.NewV7(), Kind: events.SavedPriceDropped, SubjectID: id,
				Payload: map[string]any{"price": price}, OccurredAt: at,
			}
		}
		return events.Insert(ctx, tx, evs...)
	})
	if err != nil {
		return 0, fmt.Errorf("price moved: %w", err)
	}
	return len(told), nil
}

func (s pgSaves) Counts(ctx context.Context, ids []uuid.UUID, since time.Time) (map[uuid.UUID]Counts, error) {
	rows, err := s.db.Query(ctx, `SELECT listing_id, count(*)::int, (count(*) FILTER (WHERE created_at >= @since))::int
FROM saved_listings WHERE listing_id = ANY(@ids) GROUP BY listing_id`, pgx.StrictNamedArgs{"ids": ids, "since": since})
	if err != nil {
		return nil, fmt.Errorf("count saves: %w", err)
	}
	return collectCounts(rows)
}

func (s pgSaves) Recent(ctx context.Context, account uuid.UUID, since time.Time, limit int) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `SELECT listing_id FROM saved_listings
WHERE account_id = @account AND created_at >= @since ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"account": account, "since": since, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select recent saves: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read recent saves: %w", err)
	}
	return ids, nil
}

// collectCounts reads rows of a listing ID and its two counts.
func collectCounts(rows pgx.Rows) (map[uuid.UUID]Counts, error) {
	out := map[uuid.UUID]Counts{}
	var (
		id uuid.UUID
		c  Counts
	)
	if _, err := pgx.ForEachRow(rows, []any{&id, &c.All, &c.Since}, func() error {
		out[id] = c
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read counts: %w", err)
	}
	return out, nil
}
