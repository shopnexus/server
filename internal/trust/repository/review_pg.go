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
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/trust/domain"
)

type pgReviews struct{ db *pgxpool.Pool }

func NewReviews(db *pgxpool.Pool) Reviews { return pgReviews{db} }

const reviewColumns = `id, order_id, role, reviewer_id, subject_id, listing_id, stars, body, tags, photos, reply, replied_at,
       visible_at, deleted_at, write_id, created_at, updated_at`

func scanReview(row pgx.Row) (domain.Review, error) {
	var (
		st    domain.ReviewState
		reply *string
	)
	if err := row.Scan(&st.ID, &st.OrderID, &st.Role, &st.ReviewerID, &st.SubjectID, &st.ListingID, &st.Stars, &st.Text,
		&st.Tags, &st.Photos, &reply, &st.RepliedAt, &st.VisibleAt, &st.DeletedAt, &st.WriteID, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return domain.Review{}, fmt.Errorf("scan review: %w", err)
	}
	if reply != nil {
		st.Reply = *reply
	}
	if len(st.Tags) == 0 {
		st.Tags = nil
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
       tags, photos, visible_at, deleted_at, write_id, created_at, updated_at)
VALUES (@id, @order, @role, @reviewer, @subject, @listing, @stars, @body, @tags, @photos, @visible_at, @deleted_at, @write_id,
        @created_at, @updated_at)
ON CONFLICT (order_id, role) DO NOTHING`, pgx.StrictNamedArgs{
			"id": st.ID, "order": st.OrderID, "role": st.Role, "reviewer": st.ReviewerID, "subject": st.SubjectID,
			"listing": st.ListingID, "stars": st.Stars, "body": st.Text, "tags": tagsOf(st), "photos": photosOf(st),
			"visible_at": st.VisibleAt,
			"deleted_at": st.DeletedAt,
			"write_id":   st.WriteID, "created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
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
		err := tx.QueryRow(ctx, `UPDATE reviews
SET stars = @stars, body = @body, tags = @tags, photos = @photos, deleted_at = @deleted_at, updated_at = @updated_at, reply = @reply,
    replied_at = @replied_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)
RETURNING old.replied_at IS NOT NULL`, pgx.StrictNamedArgs{
			"id": st.ID, "stars": st.Stars, "body": st.Text, "tags": tagsOf(st), "photos": photosOf(st), "deleted_at": st.DeletedAt, "updated_at": st.UpdatedAt,
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

func (s pgReviews) TagCounts(ctx context.Context, subject uuid.UUID, role domain.Role, now time.Time) ([]TagCount, error) {
	rows, err := s.db.Query(ctx, `SELECT tag, count(*) FROM reviews, unnest(tags) AS tag
WHERE subject_id = @subject AND role = @role AND deleted_at IS NULL AND visible_at <= @now
GROUP BY tag ORDER BY count(*) DESC, tag COLLATE "C"`,
		pgx.StrictNamedArgs{"subject": subject, "role": role, "now": now})
	if err != nil {
		return nil, fmt.Errorf("count review tags: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TagCount, error) {
		var c TagCount
		if scanErr := row.Scan(&c.Tag, &c.Count); scanErr != nil {
			return TagCount{}, fmt.Errorf("scan review tag: %w", scanErr)
		}
		return c, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read review tags: %w", err)
	}
	return out, nil
}

// tagsOf is st's tags for the NOT NULL text[] column: {} without any.
func tagsOf(st domain.ReviewState) []domain.Tag {
	if st.Tags == nil {
		return []domain.Tag{}
	}
	return st.Tags
}

// photosOf is st's photos for the NOT NULL jsonb column: [] without any.
func photosOf(st domain.ReviewState) []domain.Photo {
	if st.Photos == nil {
		return []domain.Photo{}
	}
	return st.Photos
}

func (s pgReviews) MarkHelpful(ctx context.Context, review, account uuid.UUID, at time.Time) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO review_helpful (review_id, account_id, created_at) VALUES (@review, @account, @at)
ON CONFLICT (review_id, account_id) DO NOTHING`, pgx.StrictNamedArgs{"review": review, "account": account, "at": at}); err != nil {
		return fmt.Errorf("mark helpful: %w", err)
	}
	return nil
}

func (s pgReviews) UnmarkHelpful(ctx context.Context, review, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM review_helpful WHERE review_id = @review AND account_id = @account`,
		pgx.StrictNamedArgs{"review": review, "account": account}); err != nil {
		return fmt.Errorf("unmark helpful: %w", err)
	}
	return nil
}

func (s pgReviews) Helpful(ctx context.Context, reviews []uuid.UUID, viewer *uuid.UUID) (map[uuid.UUID]HelpfulMarks, error) {
	rows, err := s.db.Query(ctx, `SELECT review_id, count(*), coalesce(bool_or(account_id = @viewer), false) FROM review_helpful
WHERE review_id = ANY(@reviews) GROUP BY review_id`, pgx.StrictNamedArgs{"reviews": reviews, "viewer": viewer})
	if err != nil {
		return nil, fmt.Errorf("count helpful marks: %w", err)
	}
	out := map[uuid.UUID]HelpfulMarks{}
	var (
		id uuid.UUID
		m  HelpfulMarks
	)
	if _, err = pgx.ForEachRow(rows, []any{&id, &m.Count, &m.Viewer}, func() error {
		out[id] = m
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read helpful marks: %w", err)
	}
	return out, nil
}

func (s pgReviews) CountByRole(ctx context.Context) (map[domain.Role]int, error) {
	return postgres.Counts[domain.Role](s.db.Query(ctx, `SELECT role, count(*) FROM reviews WHERE deleted_at IS NULL GROUP BY role`))
}
