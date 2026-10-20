package otp

import (
	"context"
	"errors"
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
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO NOTHING`

func (s challengePG) Create(ctx context.Context, c Challenge) error {
	_, err := s.db.Exec(ctx, createChallenge, c.id, c.purpose, c.dest.Kind(), c.dest.Value(), c.accountID,
		c.codeHash, c.ip, c.expiresAt, c.createdAt, c.writeID)
	return err
}

const getChallenge = `
SELECT id, purpose, kind, destination, account_id, code_hash, attempts, ip, expires_at, created_at, consumed_at, write_id
FROM otp_challenges WHERE id = $1`

func (s challengePG) Get(ctx context.Context, id uuid.UUID) (Challenge, error) {
	var (
		c    Challenge
		kind contact.Kind
		dest string
		ip   netip.Prefix
	)
	err := s.db.QueryRow(ctx, getChallenge, id).Scan(&c.id, &c.purpose, &kind, &dest, &c.accountID, &c.codeHash,
		&c.attempts, &ip, &c.expiresAt, &c.createdAt, &c.consumedAt, &c.writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Challenge{}, ErrNotFound
	}
	if err != nil {
		return Challenge{}, err
	}
	c.dest, c.ip = contact.Restore(kind, dest), ip.Addr()
	return c, nil
}

const saveChallenge = `
UPDATE otp_challenges SET attempts = $3, consumed_at = $4, write_id = $5
WHERE id = $1 AND (write_id = $2 OR write_id = $5)`

func (s challengePG) Save(ctx context.Context, c *Challenge, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, saveChallenge, c.id, c.writeID, c.attempts, c.consumedAt, writeID)
	if err != nil {
		return err
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
    WHERE destination = $1 AND purpose = $2 AND (created_at, id) > ($3, $4))`

func (s challengePG) IsNewest(ctx context.Context, c Challenge) (bool, error) {
	var newest bool
	err := s.db.QueryRow(ctx, isNewest, c.dest.Value(), c.purpose, c.createdAt, c.id).Scan(&newest)
	return newest, err
}
