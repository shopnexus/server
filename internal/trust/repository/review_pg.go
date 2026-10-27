package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/trust/domain"
)

type pgReviews struct{ db *pgxpool.Pool }

func NewReviews(db *pgxpool.Pool) Reviews { return pgReviews{db} }

const reviewColumns = `id, order_id, seller_id, buyer_id, listing_id, stars, body, deleted_at, write_id, created_at,
       updated_at`

func scanReview(row pgx.Row) (domain.Review, error) {
	var st domain.ReviewState
	if err := row.Scan(&st.ID, &st.OrderID, &st.SellerID, &st.BuyerID, &st.ListingID, &st.Stars, &st.Text,
		&st.DeletedAt, &st.WriteID, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return domain.Review{}, fmt.Errorf("scan review: %w", err)
	}
	return domain.RestoreReview(st), nil
}

func (s pgReviews) ByOrder(ctx context.Context, order uuid.UUID) (domain.Review, error) {
	r, err := scanReview(s.db.QueryRow(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE order_id = @order`,
		pgx.StrictNamedArgs{"order": order}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Review{}, ErrNotFound
	}
	if err != nil {
		return domain.Review{}, err
	}
	return r, nil
}

func (s pgReviews) Insert(ctx context.Context, r domain.Review) error {
	st := r.State()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO reviews (`+reviewColumns+`)
VALUES (@id, @order, @seller, @buyer, @listing, @stars, @body, @deleted_at, @write_id, @created_at, @updated_at)
ON CONFLICT (order_id) DO NOTHING`, pgx.StrictNamedArgs{
			"id": st.ID, "order": st.OrderID, "seller": st.SellerID, "buyer": st.BuyerID, "listing": st.ListingID,
			"stars": st.Stars, "body": st.Text, "deleted_at": st.DeletedAt, "write_id": st.WriteID,
			"created_at": st.CreatedAt, "updated_at": st.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert review: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
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
	tag, err := s.db.Exec(ctx, `UPDATE reviews
SET stars = @stars, body = @body, deleted_at = @deleted_at, updated_at = @updated_at, write_id = @write_id
WHERE id = @id AND write_id = @read_write_id`, pgx.StrictNamedArgs{
		"id": st.ID, "stars": st.Stars, "body": st.Text, "deleted_at": st.DeletedAt, "updated_at": st.UpdatedAt,
		"write_id": writeID, "read_write_id": st.WriteID,
	})
	if err != nil {
		return fmt.Errorf("save review: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	r.Saved(writeID)
	return nil
}

func (s pgReviews) OfSeller(ctx context.Context, seller uuid.UUID, p page.Request) ([]domain.Review, error) {
	rows, err := s.db.Query(ctx, `SELECT `+reviewColumns+` FROM reviews
WHERE seller_id = @seller AND deleted_at IS NULL AND id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"seller": seller, "before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select reviews: %w", err)
	}
	defer rows.Close()
	var out []domain.Review
	for rows.Next() {
		r, scanErr := scanReview(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read reviews: %w", err)
	}
	return out, nil
}

func (s pgReviews) Summary(ctx context.Context, seller uuid.UUID) (average float64, count int, err error) {
	if err = s.db.QueryRow(ctx, `SELECT coalesce(avg(stars), 0)::float8, count(*) FROM reviews WHERE seller_id = @seller AND deleted_at IS NULL`,
		pgx.StrictNamedArgs{"seller": seller}).Scan(&average, &count); err != nil {
		return 0, 0, fmt.Errorf("summarize reviews: %w", err)
	}
	return average, count, nil
}
