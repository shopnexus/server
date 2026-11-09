package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/pgvec"
)

type pgVectors struct{ db *pgxpool.Pool }

func NewVectors(db *pgxpool.Pool) Vectors { return pgVectors{db} }

func (s pgVectors) State(ctx context.Context, listing uuid.UUID, textVersion, photoVersion string) (VectorState, error) {
	var st VectorState
	err := s.db.QueryRow(ctx, `SELECT text_hash FROM listing_text_vectors WHERE listing_id = @id AND model_version = @version`,
		pgx.StrictNamedArgs{"id": listing, "version": textVersion}).Scan(&st.TextHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return VectorState{}, fmt.Errorf("read text hash: %w", err)
	}
	rows, err := s.db.Query(ctx, `SELECT photo_key FROM listing_photo_vectors WHERE listing_id = @id AND model_version = @version`,
		pgx.StrictNamedArgs{"id": listing, "version": photoVersion})
	if err != nil {
		return VectorState{}, fmt.Errorf("read photo keys: %w", err)
	}
	if st.Photos, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return VectorState{}, fmt.Errorf("read photo keys: %w", err)
	}
	return st, nil
}

func (s pgVectors) PutText(ctx context.Context, listing uuid.UUID, version string, hash []byte, v []float32, at time.Time) error {
	_, err := s.db.Exec(ctx, `INSERT INTO listing_text_vectors (listing_id, model_version, text_hash, embedding, embedded_at)
VALUES (@id, @version, @hash, @v::halfvec(1024), @at)
ON CONFLICT (listing_id) DO UPDATE SET model_version = excluded.model_version, text_hash = excluded.text_hash,
    embedding = excluded.embedding, embedded_at = excluded.embedded_at`,
		pgx.StrictNamedArgs{"id": listing, "version": version, "hash": hash, "v": pgvec.Format(v), "at": at})
	if err != nil {
		return fmt.Errorf("write text vector: %w", err)
	}
	return nil
}

func (s pgVectors) PutPhoto(ctx context.Context, listing uuid.UUID, key, version string, v []float32, at time.Time) error {
	_, err := s.db.Exec(ctx, `INSERT INTO listing_photo_vectors (listing_id, photo_key, model_version, embedding, embedded_at)
VALUES (@id, @key, @version, @v::halfvec(1152), @at)
ON CONFLICT (listing_id, photo_key) DO UPDATE SET model_version = excluded.model_version, embedding = excluded.embedding,
    embedded_at = excluded.embedded_at`,
		pgx.StrictNamedArgs{"id": listing, "key": key, "version": version, "v": pgvec.Format(v), "at": at})
	if err != nil {
		return fmt.Errorf("write photo vector: %w", err)
	}
	return nil
}

func (s pgVectors) DropPhotosExcept(ctx context.Context, listing uuid.UUID, keys []string) error {
	if keys == nil {
		keys = []string{}
	}
	_, err := s.db.Exec(ctx, `DELETE FROM listing_photo_vectors WHERE listing_id = @id AND NOT (photo_key = ANY(@keys))`,
		pgx.StrictNamedArgs{"id": listing, "keys": keys})
	if err != nil {
		return fmt.Errorf("drop photo vectors: %w", err)
	}
	return nil
}

func (s pgVectors) Source(ctx context.Context, listing uuid.UUID, textVersion, photoVersion string) (text, photo []float32, err error) {
	var t, p *string
	err = s.db.QueryRow(ctx, `SELECT
    (SELECT embedding::text FROM listing_text_vectors WHERE listing_id = @id AND model_version = @tv),
    (SELECT (l2_normalize(avg(embedding::vector)))::halfvec(1152)::text FROM listing_photo_vectors
     WHERE listing_id = @id AND model_version = @pv)`,
		pgx.StrictNamedArgs{"id": listing, "tv": textVersion, "pv": photoVersion}).Scan(&t, &p)
	if err != nil {
		return nil, nil, fmt.Errorf("read source vectors: %w", err)
	}
	if t != nil {
		if text, err = pgvec.Parse(*t); err != nil {
			return nil, nil, fmt.Errorf("read the source text vector: %w", err)
		}
	}
	if p != nil {
		if photo, err = pgvec.Parse(*p); err != nil {
			return nil, nil, fmt.Errorf("read the source photo vector: %w", err)
		}
	}
	return text, photo, nil
}

func (s pgVectors) Embeddable(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM listings WHERE NOT (status = ANY(@skip)) ORDER BY id`,
		pgx.StrictNamedArgs{"skip": []string{
			string(domain.StatusDeleted), string(domain.StatusProcessing), string(domain.StatusRejected),
		}})
	if err != nil {
		return nil, fmt.Errorf("list listings: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read listing IDs: %w", err)
	}
	return ids, nil
}
