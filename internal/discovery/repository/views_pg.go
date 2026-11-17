package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
)

type pgViews struct{ db *pgxpool.Pool }

func NewViews(db *pgxpool.Pool) Views { return pgViews{db} }

// viewsLock names a viewer's marks for an advisory lock, which stands in
// for a lock on the accounts row: Record holds it shared and Clear
// exclusively, so a record in flight when the account is deleted commits
// before Clear deletes, and one that starts after waits for Clear and then
// reads the deleted account.
const viewsLock = `hashtext('views:' || @viewer::text)`

// recordView keeps day as the viewer's last view of a public listing that
// is not theirs, while the viewer's account is not deleted, and counts the
// view only when the mark moved: a second view that day finds the mark at
// day already, and ON CONFLICT makes a concurrent one wait for the first
// and then find it so.
const recordView = `
WITH mark AS (
    INSERT INTO listing_view_marks (listing_id, viewer_id, viewed_on)
    SELECT l.id, a.id, @day::date
    FROM listings l
    JOIN accounts a ON a.id = @viewer AND a.status <> 'deleted'
    WHERE l.id = @listing AND l.seller_id <> a.id AND l.status = ANY(@public)
    ON CONFLICT (listing_id, viewer_id) DO UPDATE SET viewed_on = excluded.viewed_on
    WHERE listing_view_marks.viewed_on < excluded.viewed_on
    RETURNING listing_id
)
INSERT INTO listing_view_days (listing_id, viewed_on, view_count)
SELECT listing_id, @day::date, 1 FROM mark
ON CONFLICT (listing_id, viewed_on) DO UPDATE SET view_count = listing_view_days.view_count + 1`

// Record sends the lock and the record as one batch: one round trip, and
// one implicit transaction that holds the lock until the record commits.
// The record is a statement of its own, so it reads the account after the
// lock is held.
func (s pgViews) Record(ctx context.Context, listing, viewer uuid.UUID, day time.Time) error {
	b := &pgx.Batch{}
	b.Queue(`SELECT pg_advisory_xact_lock_shared(`+viewsLock+`)`, pgx.StrictNamedArgs{"viewer": viewer})
	b.Queue(recordView, pgx.StrictNamedArgs{"listing": listing, "viewer": viewer, "day": day, "public": catalogdomain.Public})
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("record view: %w", err)
	}
	return nil
}

func (s pgViews) Counts(ctx context.Context, ids []uuid.UUID, from time.Time) (map[uuid.UUID]Counts, error) {
	rows, err := s.db.Query(ctx, `SELECT listing_id, sum(view_count)::int, coalesce(sum(view_count) FILTER (WHERE viewed_on >= @from::date), 0)::int
FROM listing_view_days WHERE listing_id = ANY(@ids) GROUP BY listing_id`, pgx.StrictNamedArgs{"ids": ids, "from": from})
	if err != nil {
		return nil, fmt.Errorf("count views: %w", err)
	}
	return collectCounts(rows)
}

func (s pgViews) Recent(ctx context.Context, viewer uuid.UUID, from time.Time, limit int) ([]domain.ViewMark, error) {
	rows, err := s.db.Query(ctx, `SELECT listing_id, viewed_on FROM listing_view_marks
WHERE viewer_id = @viewer AND viewed_on >= @from::date ORDER BY viewed_on DESC, listing_id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"viewer": viewer, "from": from, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select recent views: %w", err)
	}
	marks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ViewMark, error) {
		var m domain.ViewMark
		return m, row.Scan(&m.Listing, &m.Day)
	})
	if err != nil {
		return nil, fmt.Errorf("read recent views: %w", err)
	}
	return marks, nil
}

func (s pgViews) Clear(ctx context.Context, viewer uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(`+viewsLock+`)`, pgx.StrictNamedArgs{"viewer": viewer}); err != nil {
			return fmt.Errorf("lock the viewer's marks: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM listing_view_marks WHERE viewer_id = @viewer`, pgx.StrictNamedArgs{"viewer": viewer}); err != nil {
			return fmt.Errorf("delete marks: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("clear views: %w", err)
	}
	return nil
}
