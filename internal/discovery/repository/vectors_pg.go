package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/pgvec"
)

type pgQueryVectors struct{ db *pgxpool.Pool }

func NewQueryVectors(db *pgxpool.Pool) QueryVectors { return pgQueryVectors{db} }

func textHash(text string) []byte {
	h := sha256.Sum256([]byte(domain.QueryKey(text)))
	return h[:]
}

func (s pgQueryVectors) Get(ctx context.Context, model, text string, now time.Time) (vec []float32, found bool, err error) {
	var (
		v    string
		used time.Time
	)
	args := pgx.StrictNamedArgs{"model": model, "hash": textHash(text)}
	err = s.db.QueryRow(ctx, `SELECT embedding::text, last_used_at FROM search_text_vectors
WHERE model_version = @model AND text_hash = @hash`, args).Scan(&v, &used)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read text vector: %w", err)
	}
	if vec, err = pgvec.Parse(v); err != nil {
		return nil, false, fmt.Errorf("read the cached vector: %w", err)
	}
	if now.Sub(used) >= domain.VectorTouchEvery {
		args["now"] = now
		if _, err = s.db.Exec(ctx, `UPDATE search_text_vectors SET last_used_at = @now, hits = hits + 1
WHERE model_version = @model AND text_hash = @hash`, args); err != nil {
			return nil, false, fmt.Errorf("count a use of the text vector: %w", err)
		}
	}
	return vec, true, nil
}

func (s pgQueryVectors) Put(ctx context.Context, model, text string, v []float32, now time.Time) error {
	_, err := s.db.Exec(ctx, `INSERT INTO search_text_vectors (model_version, text_hash, embedding, created_at, last_used_at)
VALUES (@model, @hash, @v::halfvec, @now, @now)
ON CONFLICT (model_version, text_hash) DO UPDATE SET embedding = excluded.embedding`,
		pgx.StrictNamedArgs{"model": model, "hash": textHash(text), "v": pgvec.Format(v), "now": now})
	if err != nil {
		return fmt.Errorf("store text vector: %w", err)
	}
	return nil
}

func (s pgQueryVectors) Sweep(ctx context.Context, now time.Time, keepUnused, keepUsed time.Duration, limit int) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM search_text_vectors
WHERE last_used_at < @used OR (hits = 0 AND last_used_at < @unused)`,
			pgx.StrictNamedArgs{"used": now.Add(-keepUsed), "unused": now.Add(-keepUnused)})
		if err != nil {
			return fmt.Errorf("delete the unused: %w", err)
		}
		n = tag.RowsAffected()
		tag, err = tx.Exec(ctx, `DELETE FROM search_text_vectors WHERE ctid IN (
	SELECT ctid FROM search_text_vectors ORDER BY hits, last_used_at
	LIMIT greatest((SELECT count(*) FROM search_text_vectors) - @max, 0))`, pgx.StrictNamedArgs{"max": limit})
		if err != nil {
			return fmt.Errorf("delete past the cap: %w", err)
		}
		n += tag.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("sweep text vectors: %w", err)
	}
	return n, nil
}

type pgPhotoVectors struct{ db *pgxpool.Pool }

func NewPhotoVectors(db *pgxpool.Pool) PhotoVectors { return pgPhotoVectors{db} }

func (s pgPhotoVectors) Get(ctx context.Context, upload uuid.UUID, model string, now time.Time) (vec []float32, found bool, err error) {
	var text string
	err = s.db.QueryRow(ctx, `SELECT embedding::text FROM search_photo_vectors
WHERE upload_id = @id AND model_version = @model AND expires_at > @now`,
		pgx.StrictNamedArgs{"id": upload, "model": model, "now": now}).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read photo vector: %w", err)
	}
	if vec, err = pgvec.Parse(text); err != nil {
		return nil, false, fmt.Errorf("read the cached vector: %w", err)
	}
	return vec, true, nil
}

func (s pgPhotoVectors) Put(ctx context.Context, upload uuid.UUID, model string, v []float32, expires, now time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM search_photo_vectors WHERE expires_at <= @now`,
			pgx.StrictNamedArgs{"now": now}); err != nil {
			return fmt.Errorf("delete expired: %w", err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO search_photo_vectors (upload_id, model_version, embedding, expires_at)
VALUES (@id, @model, @v::halfvec(1152), @expires)
ON CONFLICT (upload_id) DO UPDATE SET model_version = excluded.model_version, embedding = excluded.embedding, expires_at = excluded.expires_at`,
			pgx.StrictNamedArgs{"id": upload, "model": model, "v": pgvec.Format(v), "expires": expires})
		if err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store photo vector: %w", err)
	}
	return nil
}
