package repository

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/platform/contact"
)

type pgChallenges struct{ db *pgxpool.Pool }

func NewChallenges(db *pgxpool.Pool) Challenges { return pgChallenges{db} }

const createChallenge = `
INSERT INTO otp_challenges (id, purpose, kind, destination, account_id, code_hash, ip, expires_at, created_at, write_id)
VALUES (@id, @purpose, @kind, @destination, @account_id, @code_hash, @ip, @expires_at, @created_at, @write_id)
ON CONFLICT (id) DO NOTHING`

func (s pgChallenges) Create(ctx context.Context, c domain.Challenge) error {
	_, err := s.db.Exec(ctx, createChallenge, pgx.StrictNamedArgs{
		"id": c.ID(), "purpose": c.Purpose(), "kind": c.Destination().Kind(), "destination": c.Destination().Value(),
		"account_id": c.AccountID(), "code_hash": c.CodeHash(), "ip": c.IP(),
		"expires_at": c.ExpiresAt(), "created_at": c.CreatedAt(), "write_id": c.WriteID(),
	})
	if err != nil {
		return fmt.Errorf("insert challenge: %w", err)
	}
	return nil
}

const getChallenge = `
SELECT id, purpose, kind, destination, account_id, code_hash, attempts, ip, expires_at, created_at, consumed_at, write_id
FROM otp_challenges WHERE id = @id`

func (s pgChallenges) Get(ctx context.Context, id uuid.UUID) (domain.Challenge, error) {
	var (
		cid, writeID         uuid.UUID
		purpose              domain.Purpose
		kind                 contact.Kind
		dest                 string
		accountID            *uuid.UUID
		codeHash             []byte
		attempts             int
		ip                   netip.Prefix
		expiresAt, createdAt time.Time
		consumedAt           *time.Time
	)
	err := s.db.QueryRow(ctx, getChallenge, pgx.StrictNamedArgs{"id": id}).Scan(&cid, &purpose, &kind, &dest, &accountID, &codeHash,
		&attempts, &ip, &expiresAt, &createdAt, &consumedAt, &writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Challenge{}, ErrNotFound
	}
	if err != nil {
		return domain.Challenge{}, fmt.Errorf("select challenge: %w", err)
	}
	return domain.RestoreChallenge(cid, purpose, contact.Restore(kind, dest), accountID, codeHash, attempts, ip.Addr(),
		expiresAt, createdAt, consumedAt, writeID), nil
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
