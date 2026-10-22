package credentials

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct{ db *pgxpool.Pool }

func NewPGStore(db *pgxpool.Pool) Store { return pgStore{db} }

// upsertIdentity inserts an identity or refreshes its last_used_at. The WHERE
// keeps another account's identity untouched, and then no row comes back.
const upsertIdentity = `
INSERT INTO identities (id, account_id, kind, value, last_used_at, created_at)
VALUES (@id, @account_id, @kind, @value, @last_used_at, @created_at)
ON CONFLICT (kind, value) DO UPDATE SET last_used_at = EXCLUDED.last_used_at
WHERE identities.account_id = EXCLUDED.account_id
RETURNING account_id`

func upsertIdentities(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, ids []Identity) error {
	for _, i := range ids {
		var owner uuid.UUID
		err := tx.QueryRow(ctx, upsertIdentity, pgx.StrictNamedArgs{
			"id": i.id, "account_id": accountID, "kind": i.kind, "value": i.value,
			"last_used_at": i.lastUsedAt, "created_at": i.createdAt,
		}).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTaken
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s pgStore) Create(ctx context.Context, c Credentials) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
INSERT INTO credentials (account_id, write_id, updated_at) VALUES (@account_id, @write_id, @updated_at)
ON CONFLICT (account_id) DO NOTHING`,
			pgx.StrictNamedArgs{"account_id": c.accountID, "write_id": c.writeID, "updated_at": c.updatedAt}); err != nil {
			return err
		}
		return upsertIdentities(ctx, tx, c.accountID, c.identities)
	})
}

func (s pgStore) Get(ctx context.Context, accountID uuid.UUID) (Credentials, error) {
	c := Credentials{accountID: accountID}
	err := s.db.QueryRow(ctx, `SELECT write_id, updated_at FROM credentials WHERE account_id = @account_id`,
		pgx.StrictNamedArgs{"account_id": accountID}).
		Scan(&c.writeID, &c.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credentials{}, ErrNotFound
	}
	if err != nil {
		return Credentials{}, err
	}
	rows, err := s.db.Query(ctx, `
SELECT id, kind, value, last_used_at, created_at FROM identities WHERE account_id = @account_id ORDER BY created_at, id`,
		pgx.StrictNamedArgs{"account_id": accountID})
	if err != nil {
		return Credentials{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.id, &i.kind, &i.value, &i.lastUsedAt, &i.createdAt); err != nil {
			return Credentials{}, err
		}
		c.identities = append(c.identities, i)
	}
	return c, rows.Err()
}

func (s pgStore) FindIdentity(ctx context.Context, kind Kind, value string) (IdentityRef, error) {
	var ref IdentityRef
	err := s.db.QueryRow(ctx, `SELECT account_id, id FROM identities WHERE kind = @kind AND value = @value`,
		pgx.StrictNamedArgs{"kind": kind, "value": value}).
		Scan(&ref.AccountID, &ref.IdentityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return IdentityRef{}, ErrNotFound
	}
	return ref, err
}

func (s pgStore) Save(ctx context.Context, c *Credentials, writeID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
UPDATE credentials SET write_id = @write_id, updated_at = @updated_at
WHERE account_id = @account_id AND (write_id = @read_write_id OR write_id = @write_id)`, pgx.StrictNamedArgs{
			"account_id": c.accountID, "read_write_id": c.writeID, "write_id": writeID, "updated_at": c.updatedAt,
		})
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		keep := make([]uuid.UUID, len(c.identities))
		for n, i := range c.identities {
			keep[n] = i.id
		}
		if _, err := tx.Exec(ctx, `DELETE FROM identities WHERE account_id = @account_id AND NOT (id = ANY(@keep))`,
			pgx.StrictNamedArgs{"account_id": c.accountID, "keep": keep}); err != nil {
			return err
		}
		return upsertIdentities(ctx, tx, c.accountID, c.identities)
	})
	if err != nil {
		return err
	}
	c.writeID = writeID
	return nil
}
