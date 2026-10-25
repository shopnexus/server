package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/auth/domain"
)

type pgQuotas struct{ db *pgxpool.Pool }

func NewQuotas(db *pgxpool.Pool) Quotas { return pgQuotas{db} }

func (s pgQuotas) Get(ctx context.Context, key string) (domain.Quota, error) {
	var (
		sentAt  []time.Time
		writeID uuid.UUID
	)
	err := s.db.QueryRow(ctx, `SELECT sent_at, write_id FROM otp_quotas WHERE key = @key`, pgx.StrictNamedArgs{"key": key}).Scan(&sentAt, &writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewQuota(key), nil
	}
	if err != nil {
		return domain.Quota{}, fmt.Errorf("select quota: %w", err)
	}
	return domain.RestoreQuota(key, sentAt, writeID), nil
}

// saveQuota inserts a new quota or updates one whose write_id matches. A quota
// never saved carries the zero UUID, which no row holds, so two first saves
// of one key conflict.
const saveQuota = `
INSERT INTO otp_quotas (key, sent_at, write_id) VALUES (@key, @sent_at, @write_id)
ON CONFLICT (key) DO UPDATE SET sent_at = EXCLUDED.sent_at, write_id = EXCLUDED.write_id
WHERE otp_quotas.write_id = @read_write_id OR otp_quotas.write_id = @write_id`

func (s pgQuotas) Save(ctx context.Context, q *domain.Quota, writeID uuid.UUID) error {
	sentAt := q.SentAt()
	if sentAt == nil {
		sentAt = []time.Time{}
	}
	tag, err := s.db.Exec(ctx, saveQuota, pgx.StrictNamedArgs{
		"key": q.Key(), "sent_at": sentAt, "read_write_id": q.WriteID(), "write_id": writeID,
	})
	if err != nil {
		return fmt.Errorf("upsert quota: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	q.Saved(writeID)
	return nil
}
