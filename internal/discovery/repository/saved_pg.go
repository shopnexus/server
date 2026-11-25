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
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgSaves struct{ db *pgxpool.Pool }

func NewSaves(db *pgxpool.Pool) Saves { return pgSaves{db} }

func (s pgSaves) Save(ctx context.Context, id, account, listing uuid.UUID, price int64, at time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			saved bool
			n     int
		)
		b := &pgx.Batch{}
		postgres.QueueLockLiving(b, account, accountdomain.ErrSessionRevoked)
		postgres.QueueLock(b, "saved", account)
		b.Queue(`SELECT EXISTS (SELECT 1 FROM saved_listings WHERE account_id = @account AND listing_id = @listing),
       (SELECT count(*) FROM saved_listings WHERE account_id = @account)`,
			pgx.StrictNamedArgs{"account": account, "listing": listing}).QueryRow(func(row pgx.Row) error {
			if err := row.Scan(&saved, &n); err != nil {
				return fmt.Errorf("count saves: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // wrapped below
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

// listSaves orders an account's saves by whether the listing ended (its
// status is in @last), then newest first. The cursor is the ID of the last
// save listed, and its listing's status says where to go on; a save
// removed since counts as one whose listing has not ended.
const listSaves = `WITH s AS (
    SELECT s.id, s.listing_id, s.saved_price, s.created_at, s.alert_below, s.account_id, l.status = ANY(@last) AS ended
    FROM saved_listings s JOIN listings l ON l.id = s.listing_id
    WHERE s.account_id = @account
), c AS (
    SELECT coalesce((SELECT ended FROM s WHERE id = @before), false) AS ended
)
SELECT s.id, s.listing_id, s.saved_price, s.created_at, s.alert_below, s.account_id FROM s, c
WHERE s.ended > c.ended OR (s.ended = c.ended AND s.id < @before)
ORDER BY s.ended, s.id DESC LIMIT @limit`

func (s pgSaves) List(ctx context.Context, account uuid.UUID, last []catalogdomain.Status, p page.Request) ([]Save, error) {
	rows, err := s.db.Query(ctx, listSaves,
		pgx.StrictNamedArgs{"account": account, "last": last, "before": p.Before(), "limit": p.Fetch()})
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
		b := &pgx.Batch{}
		b.Queue(`UPDATE saved_listings s SET alerted_price = NULL
WHERE s.listing_id = @listing AND s.alerted_price IS NOT NULL
  AND `+alertLevel+` <= @price`, args)
		b.Queue(`UPDATE saved_listings s SET alerted_price = @price
WHERE s.listing_id = @listing AND `+alertLevel+` > @price
  AND (s.alerted_price IS NULL OR s.alerted_price > @price)
RETURNING s.id`, args).Query(func(rows pgx.Rows) error {
			var err error
			if told, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
				return fmt.Errorf("tell alerts: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		// The events need the told IDs, so they go in a second batch.
		evs := make([]events.Event, len(told))
		for i, id := range told {
			evs[i] = events.Event{
				ID: uuid.NewV7(), Kind: events.SavedPriceDropped, SubjectID: id,
				Payload: map[string]any{"price": price}, OccurredAt: at,
			}
		}
		b = &pgx.Batch{}
		events.Queue(b, evs...)
		return tx.SendBatch(ctx, b).Close()
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
