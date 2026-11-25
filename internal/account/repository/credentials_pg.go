package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/domain"
)

type pgCredentials struct{ db *pgxpool.Pool }

func NewCredentials(db *pgxpool.Pool) Credentials { return pgCredentials{db} }

// upsertIdentities inserts identities or refreshes their last_used_at.
// The WHERE keeps another account's identity untouched, and then its row
// does not come back.
const upsertIdentities = `
INSERT INTO identities (id, account_id, kind, identifier, last_used_at, created_at)
SELECT id, @account_id, kind, identifier, last_used_at, created_at
FROM unnest(@ids::uuid[], @kinds::text[], @identifiers::text[], @last_used_at::timestamptz[], @created_at::timestamptz[])
    AS i(id, kind, identifier, last_used_at, created_at)
ON CONFLICT (kind, identifier) DO UPDATE SET last_used_at = EXCLUDED.last_used_at
WHERE identities.account_id = EXCLUDED.account_id
RETURNING id`

// queueUpsertIdentities queues the upsert of ids; one taken by another
// account is ErrIdentityTaken, so the batch runs in a tx.
func queueUpsertIdentities(b *pgx.Batch, accountID uuid.UUID, ids []domain.Identity) {
	n := len(ids)
	idents, kinds, values := make([]uuid.UUID, n), make([]string, n), make([]string, n)
	lastUsed, created := make([]*time.Time, n), make([]time.Time, n)
	for k, i := range ids {
		idents[k], kinds[k], values[k], lastUsed[k], created[k] = i.ID(), string(i.Kind()), i.Value(), i.LastUsedAt(), i.CreatedAt()
	}
	b.Queue(upsertIdentities, pgx.StrictNamedArgs{
		"account_id": accountID, "ids": idents, "kinds": kinds, "identifiers": values, "last_used_at": lastUsed, "created_at": created,
	}).Fn = func(br pgx.BatchResults) error {
		rows, err := br.Query()
		if err != nil {
			return fmt.Errorf("upsert identities: %w", err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return fmt.Errorf("upsert identities: %w", err)
		}
		if len(got) < n {
			return domain.ErrIdentityTaken
		}
		return nil
	}
}

func (s pgCredentials) SignUp(ctx context.Context, a domain.Account, c domain.Credentials, key uuid.UUID) error {
	var requestKey *uuid.UUID
	if key != uuid.Nil() {
		requestKey = &key
	}
	b := &pgx.Batch{}
	queueInsertAccount(b, a)
	queue(b, "insert credentials", `
INSERT INTO credentials (account_id, write_id, updated_at, request_key)
VALUES (@account_id, @write_id, @updated_at, @request_key)
ON CONFLICT (account_id) DO NOTHING`,
		pgx.StrictNamedArgs{
			"account_id": c.AccountID(), "write_id": c.WriteID(), "updated_at": c.UpdatedAt(), "request_key": requestKey,
		}, nil)
	queueUpsertIdentities(b, c.AccountID(), c.Identities())
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("sign up: %w", err)
	}
	return nil
}

func (s pgCredentials) SignedUpWith(ctx context.Context, accountID, key uuid.UUID) (bool, error) {
	var signedUp bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM credentials WHERE account_id = @account_id AND request_key = @key)`,
		pgx.StrictNamedArgs{"account_id": accountID, "key": key}).Scan(&signedUp)
	if err != nil {
		return false, fmt.Errorf("select sign-up key: %w", err)
	}
	return signedUp, nil
}

func (s pgCredentials) Get(ctx context.Context, accountID uuid.UUID) (domain.Credentials, error) {
	var (
		writeID   uuid.UUID
		updatedAt time.Time
	)
	err := s.db.QueryRow(ctx, `SELECT write_id, updated_at FROM credentials WHERE account_id = @account_id`,
		pgx.StrictNamedArgs{"account_id": accountID}).
		Scan(&writeID, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credentials{}, ErrNotFound
	}
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("select credentials: %w", err)
	}
	rows, err := s.db.Query(ctx, `
SELECT id, kind, identifier, last_used_at, created_at FROM identities WHERE account_id = @account_id ORDER BY created_at, id`,
		pgx.StrictNamedArgs{"account_id": accountID})
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("select identities: %w", err)
	}
	identities, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Identity, error) {
		var (
			id        uuid.UUID
			kind      domain.Kind
			value     string
			lastUsed  *time.Time
			createdAt time.Time
		)
		if scanErr := row.Scan(&id, &kind, &value, &lastUsed, &createdAt); scanErr != nil {
			return domain.Identity{}, fmt.Errorf("scan identity: %w", scanErr)
		}
		return domain.RestoreIdentity(id, kind, value, lastUsed, createdAt), nil
	})
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("read identities: %w", err)
	}
	return domain.RestoreCredentials(accountID, identities, writeID, updatedAt), nil
}

func (s pgCredentials) FindIdentity(ctx context.Context, kind domain.Kind, value string) (domain.IdentityRef, error) {
	var ref domain.IdentityRef
	err := s.db.QueryRow(ctx, `SELECT account_id, id FROM identities WHERE kind = @kind AND identifier = @identifier`,
		pgx.StrictNamedArgs{"kind": kind, "identifier": value}).
		Scan(&ref.AccountID, &ref.IdentityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IdentityRef{}, ErrNotFound
	}
	if err != nil {
		return domain.IdentityRef{}, fmt.Errorf("select identity: %w", err)
	}
	return ref, nil
}

func (s pgCredentials) Contacts(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := s.db.Query(ctx, `
SELECT DISTINCT ON (account_id) account_id, identifier FROM identities
WHERE account_id = ANY(@ids) AND kind IN ('phone', 'email')
ORDER BY account_id, kind = 'phone' DESC, created_at`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select contacts: %w", err)
	}
	out := map[uuid.UUID]string{}
	var id uuid.UUID
	var value string
	if _, err = pgx.ForEachRow(rows, []any{&id, &value}, func() error {
		out[id] = value
		return nil
	}); err != nil {
		return nil, fmt.Errorf("scan contacts: %w", err)
	}
	return out, nil
}

func (s pgCredentials) Save(ctx context.Context, c *domain.Credentials, writeID uuid.UUID) error {
	identities := c.Identities()
	keep := make([]uuid.UUID, len(identities))
	for n, i := range identities {
		keep[n] = i.ID()
	}
	b := &pgx.Batch{}
	queue(b, "update credentials", `
UPDATE credentials SET write_id = @write_id, updated_at = @updated_at
WHERE account_id = @account_id AND (write_id = @read_write_id OR write_id = @write_id)`, pgx.StrictNamedArgs{
		"account_id": c.AccountID(), "read_write_id": c.WriteID(), "write_id": writeID, "updated_at": c.UpdatedAt(),
	}, ErrConflict)
	queue(b, "delete removed identities", `DELETE FROM identities WHERE account_id = @account_id AND NOT (id = ANY(@keep))`,
		pgx.StrictNamedArgs{"account_id": c.AccountID(), "keep": keep}, nil)
	queueUpsertIdentities(b, c.AccountID(), identities)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	c.Saved(writeID)
	return nil
}
