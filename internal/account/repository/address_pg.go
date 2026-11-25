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
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgAddresses struct{ db *pgxpool.Pool }

// NewAddresses returns the Postgres Addresses.
func NewAddresses(db *pgxpool.Pool) Addresses { return pgAddresses{db} }

// addressColumns are the columns scanAddress reads, of addresses a.
const addressColumns = `a.id, a.account_id, a.recipient_name, a.phone, a.street, a.commune_code, a.kind, a.is_default, a.write_id, a.created_at`

// scanAddress reads addressColumns, then into extra the columns that follow.
func scanAddress(row pgx.Row, extra ...any) (domain.Address, error) {
	var (
		id, owner, writeID uuid.UUID
		d                  domain.AddressDraft
		isDefault          bool
		createdAt          time.Time
	)
	dest := append([]any{&id, &owner, &d.Name, &d.Phone, &d.Street, &d.CommuneCode, &d.Kind, &isDefault, &writeID, &createdAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return domain.Address{}, fmt.Errorf("scan address: %w", err)
	}
	return domain.RestoreAddress(id, owner, d, isDefault, writeID, createdAt), nil
}

// queueLockBook takes owner's advisory lock until the tx ends.
func queueLockBook(b *pgx.Batch, owner uuid.UUID) {
	postgres.QueueLock(b, "addresses", owner)
}

// queueClearDefault takes the default from owner's address that holds it,
// unless that is keep, under writeID. It runs before another address
// becomes the default, since the unique index on the default is checked
// row by row.
func queueClearDefault(b *pgx.Batch, owner, keep, writeID uuid.UUID) {
	queue(b, "clear the default", `UPDATE addresses SET is_default = false, write_id = @write_id
WHERE account_id = @account AND is_default AND id <> @keep`,
		pgx.StrictNamedArgs{"account": owner, "keep": keep, "write_id": writeID}, nil)
}

func (s pgAddresses) Insert(ctx context.Context, key uuid.UUID, a *domain.Address, makeDefault bool) error {
	var (
		isDefault bool
		stored    *domain.Address
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		postgres.QueueLockLiving(b, a.AccountID(), domain.ErrSessionRevoked)
		queueLockBook(b, a.AccountID())
		b.Queue(`SELECT `+addressColumns+`
FROM addresses a WHERE a.account_id = @account AND a.request_key = @key`,
			pgx.StrictNamedArgs{"account": a.AccountID(), "key": key}).QueryRow(func(row pgx.Row) error {
			got, err := scanAddress(row)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("select the key's address: %w", err)
			}
			stored = &got
			return nil
		})
		var n int
		b.Queue(`SELECT count(*) FROM addresses WHERE account_id = @account`,
			pgx.StrictNamedArgs{"account": a.AccountID()}).QueryRow(func(row pgx.Row) error {
			if err := row.Scan(&n); err != nil {
				return fmt.Errorf("count addresses: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // the queued statements wrap their own errors.
		}
		if stored != nil {
			return nil
		}
		if n >= domain.MaxAddresses {
			return domain.ErrAddressLimit
		}
		isDefault = n == 0 || makeDefault
		b = &pgx.Batch{}
		if isDefault {
			queueClearDefault(b, a.AccountID(), a.ID(), a.WriteID())
		}
		queue(b, "insert address", `INSERT INTO addresses
    (id, account_id, recipient_name, phone, street, commune_code, kind, is_default, request_key, write_id, created_at)
VALUES (@id, @account, @name, @phone, @street, @commune_code, @kind, @is_default, @key, @write_id, @created_at)`,
			pgx.StrictNamedArgs{
				"id": a.ID(), "account": a.AccountID(), "name": a.Name(), "phone": a.Phone(), "street": a.Street(),
				"commune_code": a.CommuneCode(), "kind": a.Kind(), "is_default": isDefault, "key": key, "write_id": a.WriteID(),
				"created_at": a.CreatedAt(),
			}, nil)
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("add address: %w", err)
	}
	if stored != nil {
		*a = *stored
		return nil
	}
	if isDefault {
		a.StoredAsDefault()
	}
	return nil
}

func (s pgAddresses) Get(ctx context.Context, owner, id uuid.UUID) (domain.Address, error) {
	a, err := scanAddress(s.db.QueryRow(ctx, `SELECT `+addressColumns+`
FROM addresses a WHERE a.id = @id AND a.account_id = @account`, pgx.StrictNamedArgs{"id": id, "account": owner}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Address{}, ErrNotFound
	}
	if err != nil {
		return domain.Address{}, fmt.Errorf("select address: %w", err)
	}
	return a, nil
}

// saveAddress writes an address's fields under its write ID.
const saveAddress = `UPDATE addresses
SET recipient_name = @name, phone = @phone, street = @street, commune_code = @commune_code, kind = @kind,
    write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

func (s pgAddresses) Save(ctx context.Context, a *domain.Address, writeID uuid.UUID, makeDefault bool) error {
	args := pgx.StrictNamedArgs{
		"id": a.ID(), "read_write_id": a.WriteID(), "write_id": writeID,
		"name": a.Name(), "phone": a.Phone(), "street": a.Street(), "commune_code": a.CommuneCode(),
		"kind": a.Kind(),
	}
	if !makeDefault {
		tag, err := s.db.Exec(ctx, saveAddress, args)
		if err != nil {
			return fmt.Errorf("update address: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		a.Saved(writeID)
		return nil
	}
	// Setting the default leaves the row's write_id at writeID, which a
	// takes below.
	b := &pgx.Batch{}
	postgres.QueueLockLiving(b, a.AccountID(), domain.ErrSessionRevoked)
	queueLockBook(b, a.AccountID())
	queue(b, "update address", saveAddress, args, ErrConflict)
	queueClearDefault(b, a.AccountID(), a.ID(), writeID)
	queue(b, "set the default", `UPDATE addresses SET is_default = true WHERE id = @id`, pgx.StrictNamedArgs{"id": a.ID()}, nil)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if errors.Is(err, ErrConflict) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("save address as the default: %w", err)
	}
	a.Saved(writeID)
	a.StoredAsDefault()
	return nil
}

func (s pgAddresses) SetDefault(ctx context.Context, owner, id, writeID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		queueLockBook(b, owner)
		var isDefault bool
		b.Queue(`SELECT is_default FROM addresses WHERE id = @id AND account_id = @account`,
			pgx.StrictNamedArgs{"id": id, "account": owner}).QueryRow(func(row pgx.Row) error {
			err := row.Scan(&isDefault)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("select address: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // the queued statements wrap their own errors.
		}
		if isDefault {
			return nil
		}
		b = &pgx.Batch{}
		queueClearDefault(b, owner, id, writeID)
		queue(b, "set the default", `UPDATE addresses SET is_default = true, write_id = @write_id WHERE id = @id`,
			pgx.StrictNamedArgs{"id": id, "write_id": writeID}, nil)
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("set default address: %w", err)
	}
	return nil
}

func (s pgAddresses) Delete(ctx context.Context, owner, id, writeID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		queueLockBook(b, owner)
		var wasDefault bool
		b.Queue(`DELETE FROM addresses WHERE id = @id AND account_id = @account RETURNING is_default`,
			pgx.StrictNamedArgs{"id": id, "account": owner}).QueryRow(func(row pgx.Row) error {
			err := row.Scan(&wasDefault)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("delete address: %w", err)
			}
			return nil
		})
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err //nolint:wrapcheck // the queued statements wrap their own errors.
		}
		if !wasDefault {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE addresses SET is_default = true, write_id = @write_id
WHERE id = (SELECT id FROM addresses WHERE account_id = @account ORDER BY id DESC LIMIT 1)`,
			pgx.StrictNamedArgs{"account": owner, "write_id": writeID}); err != nil {
			return fmt.Errorf("move the default: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete address: %w", err)
	}
	return nil
}

func (s pgAddresses) List(ctx context.Context, owner uuid.UUID) ([]PlacedAddress, error) {
	rows, err := s.db.Query(ctx, `SELECT `+addressColumns+`, c.display_name, p.display_name, p.code
FROM addresses a JOIN communes c ON c.code = a.commune_code JOIN provinces p ON p.code = c.province_code
WHERE a.account_id = @account ORDER BY a.is_default DESC, a.id DESC`, pgx.StrictNamedArgs{"account": owner})
	if err != nil {
		return nil, fmt.Errorf("select addresses: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PlacedAddress, error) {
		var p Place
		a, scanErr := scanAddress(row, &p.Commune, &p.Province, &p.ProvinceCode)
		return PlacedAddress{Address: a, Place: p}, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("read addresses: %w", err)
	}
	return list, nil
}

func (s pgAddresses) Commune(ctx context.Context, code string) (Place, error) {
	var p Place
	err := s.db.QueryRow(ctx, `SELECT c.display_name, p.display_name, p.code
FROM communes c JOIN provinces p ON p.code = c.province_code WHERE c.code = @code`, pgx.StrictNamedArgs{"code": code}).
		Scan(&p.Commune, &p.Province, &p.ProvinceCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return Place{}, ErrNotFound
	}
	if err != nil {
		return Place{}, fmt.Errorf("select commune: %w", err)
	}
	return p, nil
}
