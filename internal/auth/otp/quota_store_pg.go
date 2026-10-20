package otp

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type quotaPG struct{ db *pgxpool.Pool }

func NewQuotaPGStore(db *pgxpool.Pool) QuotaStore { return quotaPG{db} }

func (s quotaPG) Get(ctx context.Context, key string) (Quota, error) {
	q := Quota{key: key}
	err := s.db.QueryRow(ctx, `SELECT sent_at, write_id FROM otp_quotas WHERE key = $1`, key).Scan(&q.sentAt, &q.writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Quota{key: key}, nil
	}
	return q, err
}

// saveQuota inserts a new quota or updates one whose write_id matches. A quota
// never saved carries the zero UUID, which no row holds, so two first saves
// of one key conflict.
const saveQuota = `
INSERT INTO otp_quotas (key, sent_at, write_id) VALUES ($1, $2, $4)
ON CONFLICT (key) DO UPDATE SET sent_at = EXCLUDED.sent_at, write_id = EXCLUDED.write_id
WHERE otp_quotas.write_id = $3 OR otp_quotas.write_id = $4`

func (s quotaPG) Save(ctx context.Context, q *Quota, writeID uuid.UUID) error {
	sentAt := q.sentAt
	if sentAt == nil {
		sentAt = []time.Time{}
	}
	tag, err := s.db.Exec(ctx, saveQuota, q.key, sentAt, q.writeID, writeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	q.writeID = writeID
	return nil
}
