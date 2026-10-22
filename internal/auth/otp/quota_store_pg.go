package otp

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type quotaPG struct{ db *pgxpool.Pool }

func NewQuotaPGStore(db *pgxpool.Pool) QuotaStore { return quotaPG{db} }

func (s quotaPG) Get(ctx context.Context, key string) (Quota, error) {
	q := NewQuota(key)
	err := s.db.QueryRow(ctx, `SELECT sent_at, write_id FROM otp_quotas WHERE key = @key`, pgx.StrictNamedArgs{"key": key}).Scan(&q.sentAt, &q.writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return NewQuota(key), nil
	}
	if err != nil {
		return Quota{}, fmt.Errorf("select quota: %w", err)
	}
	return q, nil
}

// saveQuota inserts a new quota or updates one whose write_id matches. A quota
// never saved carries the zero UUID, which no row holds, so two first saves
// of one key conflict.
const saveQuota = `
INSERT INTO otp_quotas (key, sent_at, write_id) VALUES (@key, @sent_at, @write_id)
ON CONFLICT (key) DO UPDATE SET sent_at = EXCLUDED.sent_at, write_id = EXCLUDED.write_id
WHERE otp_quotas.write_id = @read_write_id OR otp_quotas.write_id = @write_id`

func (s quotaPG) Save(ctx context.Context, q *Quota, writeID uuid.UUID) error {
	sentAt := q.sentAt
	if sentAt == nil {
		sentAt = []time.Time{}
	}
	tag, err := s.db.Exec(ctx, saveQuota, pgx.StrictNamedArgs{
		"key": q.key, "sent_at": sentAt, "read_write_id": q.writeID, "write_id": writeID,
	})
	if err != nil {
		return fmt.Errorf("upsert quota: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	q.writeID = writeID
	return nil
}
