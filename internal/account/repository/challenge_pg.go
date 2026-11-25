package repository

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/contact"
)

type pgChallenges struct{ db *pgxpool.Pool }

func NewChallenges(db *pgxpool.Pool) Challenges { return pgChallenges{db} }

const createChallenge = `
INSERT INTO otp_challenges (id, purpose, kind, destination, account_id, code_hash, ip, expires_at, created_at, request_key, write_id)
VALUES (@id, @purpose, @kind, @destination, @account_id, @code_hash, @ip, @expires_at, @created_at, @request_key, @write_id)
ON CONFLICT DO NOTHING`

func (s pgChallenges) Create(ctx context.Context, c domain.Challenge, key uuid.UUID) (uuid.UUID, error) {
	var requestKey *uuid.UUID
	if key != uuid.Nil() {
		requestKey = &key
	}
	b := &pgx.Batch{}
	b.Queue(createChallenge, pgx.StrictNamedArgs{
		"id": c.ID(), "purpose": c.Purpose(), "kind": c.Destination().Kind(), "destination": c.Destination().Value(),
		"account_id": c.AccountID(), "code_hash": c.CodeHash(), "ip": c.IP(),
		"expires_at": c.ExpiresAt(), "created_at": c.CreatedAt(), "request_key": requestKey, "write_id": c.WriteID(),
	})
	var id uuid.UUID
	b.Queue(`SELECT id FROM otp_challenges WHERE id = @id OR (destination = @destination AND request_key = @request_key)`,
		pgx.StrictNamedArgs{"id": c.ID(), "destination": c.Destination().Value(), "request_key": requestKey}).
		QueryRow(func(row pgx.Row) error { return row.Scan(&id) })
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return uuid.Nil(), fmt.Errorf("insert challenge: %w", err)
	}
	return id, nil
}

const challengeColumns = `id, purpose, kind, destination, account_id, code_hash, attempts, ip, expires_at, created_at,
       consumed_at, write_id`

func (s pgChallenges) Get(ctx context.Context, id uuid.UUID) (domain.Challenge, error) {
	return scanChallenge(s.db.QueryRow(ctx, `SELECT `+challengeColumns+` FROM otp_challenges WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}))
}

func (s pgChallenges) ByKey(ctx context.Context, dest contact.Address, key uuid.UUID) (domain.Challenge, error) {
	return scanChallenge(s.db.QueryRow(ctx, `SELECT `+challengeColumns+`
FROM otp_challenges WHERE destination = @destination AND request_key = @request_key`,
		pgx.StrictNamedArgs{"destination": dest.Value(), "request_key": key}))
}

func scanChallenge(row pgx.Row) (domain.Challenge, error) {
	var (
		st   domain.ChallengeState
		kind contact.Kind
		dest string
		ip   netip.Prefix
	)
	err := row.Scan(&st.ID, &st.Purpose, &kind, &dest, &st.AccountID, &st.CodeHash,
		&st.Attempts, &ip, &st.ExpiresAt, &st.CreatedAt, &st.ConsumedAt, &st.WriteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Challenge{}, ErrNotFound
	}
	if err != nil {
		return domain.Challenge{}, fmt.Errorf("select challenge: %w", err)
	}
	st.Dest, st.IP = contact.Restore(kind, dest), ip.Addr()
	return domain.RestoreChallenge(st), nil
}

const saveChallenge = `
UPDATE otp_challenges SET attempts = @attempts, consumed_at = @consumed_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

func (s pgChallenges) Save(ctx context.Context, c *domain.Challenge, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, saveChallenge, pgx.StrictNamedArgs{
		"id": c.ID(), "read_write_id": c.WriteID(), "write_id": writeID,
		"attempts": c.Attempts(), "consumed_at": c.ConsumedAt(),
	})
	if err != nil {
		return fmt.Errorf("update challenge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	c.Saved(writeID)
	return nil
}

const isNewest = `
SELECT NOT EXISTS (
    SELECT 1 FROM otp_challenges
    WHERE destination = @destination AND purpose = @purpose AND (created_at, id) > (@created_at, @id))`

func (s pgChallenges) IsNewest(ctx context.Context, c domain.Challenge) (bool, error) {
	var newest bool
	err := s.db.QueryRow(ctx, isNewest, pgx.StrictNamedArgs{
		"destination": c.Destination().Value(), "purpose": c.Purpose(), "created_at": c.CreatedAt(), "id": c.ID(),
	}).Scan(&newest)
	if err != nil {
		return false, fmt.Errorf("select newest challenge: %w", err)
	}
	return newest, nil
}
