package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/pgvec"
)

type pgQueryVectors struct{ db *pgxpool.Pool }

func NewQueryVectors(db *pgxpool.Pool) QueryVectors { return pgQueryVectors{db} }

func (s pgQueryVectors) Get(ctx context.Context, key string, promptVersion int, model string, since time.Time) (vec []float32, found bool, err error) {
	var text string
	err = s.db.QueryRow(ctx, `SELECT embedding::text FROM search_query_vectors
WHERE query_key = @key AND prompt_version = @version AND model_version = @model AND created_at >= @since`,
		pgx.StrictNamedArgs{"key": key, "version": promptVersion, "model": model, "since": since}).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read query vector: %w", err)
	}
	if vec, err = pgvec.Parse(text); err != nil {
		return nil, false, fmt.Errorf("read the cached vector: %w", err)
	}
	return vec, true, nil
}

func (s pgQueryVectors) Put(ctx context.Context, key string, promptVersion int, model string, v []float32, at, expired time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM search_query_vectors WHERE created_at < @expired`,
			pgx.StrictNamedArgs{"expired": expired}); err != nil {
			return fmt.Errorf("delete expired: %w", err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO search_query_vectors (query_key, prompt_version, model_version, embedding, created_at)
VALUES (@key, @version, @model, @v::halfvec, @at)
ON CONFLICT (query_key, prompt_version, model_version) DO UPDATE SET embedding = excluded.embedding, created_at = excluded.created_at`,
			pgx.StrictNamedArgs{"key": key, "version": promptVersion, "model": model, "v": pgvec.Format(v), "at": at})
		if err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store query vector: %w", err)
	}
	return nil
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
