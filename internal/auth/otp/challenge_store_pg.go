package otp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/contact"
)

type challengePG struct{ db *pgxpool.Pool }

func NewChallengePGStore(db *pgxpool.Pool) ChallengeStore { return challengePG{db} }

const createChallenge = `
INSERT INTO otp_challenges (id, purpose, kind, destination, account_id, code_hash, ip, expires_at, created_at, write_id)
VALUES (@id, @purpose, @kind, @destination, @account_id, @code_hash, @ip, @expires_at, @created_at, @write_id)
ON CONFLICT (id) DO NOTHING`

func (s challengePG) Create(ctx context.Context, c Challenge) error {
	_, err := s.db.Exec(ctx, createChallenge, pgx.StrictNamedArgs{
		"id": c.id, "purpose": c.purpose, "kind": c.dest.Kind(), "destination": c.dest.Value(),
		"account_id": c.accountID, "code_hash": c.codeHash, "ip": c.ip,
		"expires_at": c.expiresAt, "created_at": c.createdAt, "write_id": c.writeID,
	})
	if err != nil {
		return fmt.Errorf("insert challenge: %w", err)
	}
	return nil
}

const getChallenge = `
SELECT id, purpose, kind, destination, account_id, code_hash, attempts, ip, expires_at, created_at, consumed_at, write_id
FROM otp_challenges WHERE id = @id`

func (s challengePG) Get(ctx context.Context, id uuid.UUID) (Challenge, error) {
	var (
		c    Challenge
		kind contact.Kind
		dest string
		ip   netip.Prefix
	)
	err := s.db.QueryRow(ctx, getChallenge, pgx.StrictNamedArgs{"id": id}).Scan(&c.id, &c.purpose, &kind, &dest, &c.accountID, &c.codeHash,
		&c.attempts, &ip, &c.expiresAt, &c.createdAt, &c.consumedAt, &c.writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Challenge{}, ErrNotFound
	}
	if err != nil {
		return Challenge{}, fmt.Errorf("select challenge: %w", err)
	}
	c.dest, c.ip = contact.Restore(kind, dest), ip.Addr()
	return c, nil
}

const saveChallenge = `
UPDATE otp_challenges SET attempts = @attempts, consumed_at = @consumed_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

func (s challengePG) Save(ctx context.Context, c *Challenge, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, saveChallenge, pgx.StrictNamedArgs{
		"id": c.id, "read_write_id": c.writeID, "write_id": writeID,
		"attempts": c.attempts, "consumed_at": c.consumedAt,
	})
	if err != nil {
		return fmt.Errorf("update challenge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	c.writeID = writeID
	return nil
}

const isNewest = `
SELECT NOT EXISTS (
    SELECT 1 FROM otp_challenges
    WHERE destination = @destination AND purpose = @purpose AND (created_at, id) > (@created_at, @id))`

func (s challengePG) IsNewest(ctx context.Context, c Challenge) (bool, error) {
	var newest bool
	err := s.db.QueryRow(ctx, isNewest, pgx.StrictNamedArgs{
		"destination": c.dest.Value(), "purpose": c.purpose, "created_at": c.createdAt, "id": c.id,
	}).Scan(&newest)
	if err != nil {
		return false, fmt.Errorf("select newest challenge: %w", err)
	}
	return newest, nil
}
