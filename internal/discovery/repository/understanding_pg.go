package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgUnderstandings struct{ db *pgxpool.Pool }

func NewUnderstandings(db *pgxpool.Pool) Understandings { return pgUnderstandings{db} }

func (s pgUnderstandings) Get(ctx context.Context, key string, version int, since time.Time) (answer json.RawMessage, found bool, err error) {
	err = s.db.QueryRow(ctx, `SELECT answer FROM search_understandings
WHERE query_key = @key AND prompt_version = @version AND created_at >= @since`,
		pgx.StrictNamedArgs{"key": key, "version": version, "since": since}).Scan(&answer)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read answer: %w", err)
	}
	return answer, true, nil
}

func (s pgUnderstandings) Put(ctx context.Context, key string, version int, answer json.RawMessage, at, expired time.Time) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM search_understandings WHERE created_at < @expired`,
			pgx.StrictNamedArgs{"expired": expired}); err != nil {
			return fmt.Errorf("delete expired: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO search_understandings (query_key, prompt_version, answer, created_at)
VALUES (@key, @version, @answer, @at)
ON CONFLICT (query_key, prompt_version) DO UPDATE SET answer = excluded.answer, created_at = excluded.created_at`,
			pgx.StrictNamedArgs{"key": key, "version": version, "answer": answer, "at": at}); err != nil {
			return fmt.Errorf("insert answer: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store answer: %w", err)
	}
	return nil
}
