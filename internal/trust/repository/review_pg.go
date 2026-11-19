package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
)

type pgReviews struct{ db *pgxpool.Pool }

func NewReviews(db *pgxpool.Pool) Reviews { return pgReviews{db} }

const reviewColumns = `id, order_id, role, reviewer_id, subject_id, listing_id, stars, body, reply, replied_at, visible_at,
       deleted_at, write_id, created_at, updated_at`

func scanReview(row pgx.Row) (domain.Review, error) {
	var (
		st    domain.ReviewState
		reply *string
	)
	if err := row.Scan(&st.ID, &st.OrderID, &st.Role, &st.ReviewerID, &st.SubjectID, &st.ListingID, &st.Stars, &st.Text,
		&reply, &st.RepliedAt, &st.VisibleAt, &st.DeletedAt, &st.WriteID, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return domain.Review{}, fmt.Errorf("scan review: %w", err)
	}
	if reply != nil {
		st.Reply = *reply
	}
	return domain.RestoreReview(st), nil
}

func collectReviews(rows pgx.Rows) ([]domain.Review, error) {
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Review, error) { return scanReview(row) })
	if err != nil {
		return nil, fmt.Errorf("read reviews: %w", err)
	}
	return out, nil
}

func (s pgReviews) Get(ctx context.Context, id uuid.UUID) (domain.Review, error) {
	r, err := scanReview(s.db.QueryRow(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Review{}, ErrNotFound
	}
	return r, err
}

func (s pgReviews) ByOrder(ctx context.Context, order uuid.UUID, role domain.Role) (domain.Review, error) {
	r, err := scanReview(s.db.QueryRow(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE order_id = @order AND role = @role`,
		pgx.StrictNamedArgs{"order": order, "role": role}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Review{}, ErrNotFound
	}
	return r, err
}

func (s pgReviews) OfOrders(ctx context.Context, orders []uuid.UUID) ([]domain.Review, error) {
	rows, err := s.db.Query(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE order_id = ANY(@orders)`,
		pgx.StrictNamedArgs{"orders": orders})
	if err != nil {
		return nil, fmt.Errorf("select reviews: %w", err)
	}
	return collectReviews(rows)
}

func (s pgReviews) Insert(ctx context.Context, r domain.Review) error {
	st := r.State()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// One order's two reviews are written one at a time, so the second
		// always sees the first.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(@order::text, 0))`,
			pgx.StrictNamedArgs{"order": st.OrderID}); err != nil {
			return fmt.Errorf("lock the order's reviews: %w", err)
		}
		var other bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM reviews WHERE order_id = @order AND role <> @role)`,
			pgx.StrictNamedArgs{"order": st.OrderID, "role": st.Role}).Scan(&other); err != nil {
			return fmt.Errorf("read the other side's review: %w", err)
		}
		if other && st.VisibleAt.After(st.CreatedAt) {
			// The other side reviewed after the caller read; a retry shows both.
			return ErrConflict
		}
		tag, err := tx.Exec(ctx, `INSERT INTO reviews (id, order_id, role, reviewer_id, subject_id, listing_id, stars, body,
       visible_at, deleted_at, write_id, created_at, updated_at)
VALUES (@id, @order, @role, @reviewer, @subject, @listing, @stars, @body, @visible_at, @deleted_at, @write_id, @created_at,
        @updated_at)
ON CONFLICT (order_id, role) DO NOTHING`, pgx.StrictNamedArgs{
			"id": st.ID, "order": st.OrderID, "role": st.Role, "reviewer": st.ReviewerID, "subject": st.SubjectID,
			"listing": st.ListingID, "stars": st.Stars, "body": st.Text, "visible_at": st.VisibleAt, "deleted_at": st.DeletedAt,
			"write_id": st.WriteID, "created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert review: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		if _, err = tx.Exec(ctx, `UPDATE reviews SET visible_at = @at WHERE order_id = @order AND role <> @role AND visible_at > @at`,
			pgx.StrictNamedArgs{"order": st.OrderID, "role": st.Role, "at": st.CreatedAt}); err != nil {
			return fmt.Errorf("reveal the other side's review: %w", err)
		}
		return events.Insert(ctx, tx, events.Event{ID: st.ID, Kind: events.ReviewPosted, SubjectID: st.ID, OccurredAt: st.CreatedAt})
	})
	if errors.Is(err, ErrConflict) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("add review: %w", err)
	}
	return nil
}

func (s pgReviews) Save(ctx context.Context, r *domain.Review, writeID uuid.UUID) error {
	st := r.State()
	var reply *string
	if st.RepliedAt != nil {
		reply = &st.Reply
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var hadReply bool
		err := tx.QueryRow(ctx, `UPDATE reviews new
SET stars = @stars, body = @body, deleted_at = @deleted_at, updated_at = @updated_at, reply = @reply, replied_at = @replied_at,
    write_id = @write_id
FROM reviews old
WHERE new.id = @id AND old.id = new.id AND new.write_id = @read_write_id
RETURNING old.replied_at IS NOT NULL`, pgx.StrictNamedArgs{
			"id": st.ID, "stars": st.Stars, "body": st.Text, "deleted_at": st.DeletedAt, "updated_at": st.UpdatedAt,
			"reply": reply, "replied_at": st.RepliedAt, "write_id": writeID, "read_write_id": st.WriteID,
		}).Scan(&hadReply)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return fmt.Errorf("save review: %w", err)
		}
		if st.RepliedAt == nil || hadReply {
			return nil
		}
		return events.Insert(ctx, tx, events.Event{ID: writeID, Kind: events.ReviewReplied, SubjectID: st.ID, OccurredAt: *st.RepliedAt})
	})
	if errors.Is(err, ErrConflict) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("save review: %w", err)
	}
	r.Saved(writeID)
	return nil
}

func (s pgReviews) OfSubject(ctx context.Context, subject uuid.UUID, role domain.Role, now time.Time, p page.Request) ([]domain.Review, error) {
	rows, err := s.db.Query(ctx, `SELECT `+reviewColumns+` FROM reviews
WHERE subject_id = @subject AND role = @role AND deleted_at IS NULL AND visible_at <= @now AND id < @before
ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"subject": subject, "role": role, "now": now, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select reviews: %w", err)
	}
	return collectReviews(rows)
}

func (s pgReviews) Summary(ctx context.Context, subject uuid.UUID, role domain.Role, now time.Time) (average float64, count int, err error) {
	if err = s.db.QueryRow(ctx, `SELECT coalesce(avg(stars), 0)::float8, count(*) FROM reviews
WHERE subject_id = @subject AND role = @role AND deleted_at IS NULL AND visible_at <= @now`,
		pgx.StrictNamedArgs{"subject": subject, "role": role, "now": now}).Scan(&average, &count); err != nil {
		return 0, 0, fmt.Errorf("summarize reviews: %w", err)
	}
	return average, count, nil
}
