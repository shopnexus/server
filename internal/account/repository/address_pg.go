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

// lockBook takes owner's advisory lock until tx ends.
func lockBook(ctx context.Context, tx pgx.Tx, owner uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(@account::text))`,
		pgx.StrictNamedArgs{"account": owner}); err != nil {
		return fmt.Errorf("lock account: %w", err)
	}
	return nil
}

// clearDefault takes the default from owner's address that holds it, unless
// that is keep. It runs before another address becomes the default, since
// the unique index on the default is checked row by row.
func clearDefault(ctx context.Context, tx pgx.Tx, owner, keep uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE addresses SET is_default = false, write_id = uuidv7()
WHERE account_id = @account AND is_default AND id <> @keep`, pgx.StrictNamedArgs{"account": owner, "keep": keep}); err != nil {
		return fmt.Errorf("clear the default: %w", err)
	}
	return nil
}

func (s pgAddresses) Insert(ctx context.Context, key uuid.UUID, a *domain.Address, makeDefault bool) error {
	var (
		isDefault bool
		stored    *domain.Address
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockLiving(ctx, tx, a.AccountID()); err != nil {
			return err
		}
		if err := lockBook(ctx, tx, a.AccountID()); err != nil {
			return err
		}
		got, err := scanAddress(tx.QueryRow(ctx, `SELECT `+addressColumns+`
FROM addresses a WHERE a.account_id = @account AND a.request_key = @key`,
			pgx.StrictNamedArgs{"account": a.AccountID(), "key": key}))
		if err == nil {
			stored = &got
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("select the key's address: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM addresses WHERE account_id = @account`,
			pgx.StrictNamedArgs{"account": a.AccountID()}).Scan(&n); err != nil {
			return fmt.Errorf("count addresses: %w", err)
		}
		if n >= domain.MaxAddresses {
			return domain.ErrAddressLimit
		}
		isDefault = n == 0 || makeDefault
		if isDefault {
			if err := clearDefault(ctx, tx, a.AccountID(), a.ID()); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO addresses
    (id, account_id, recipient_name, phone, street, commune_code, kind, is_default, request_key, write_id, created_at)
VALUES (@id, @account, @name, @phone, @street, @commune_code, @kind, @is_default, @key, @write_id, @created_at)`,
			pgx.StrictNamedArgs{
				"id": a.ID(), "account": a.AccountID(), "name": a.Name(), "phone": a.Phone(), "street": a.Street(),
				"commune_code": a.CommuneCode(), "kind": a.Kind(), "is_default": isDefault, "key": key, "write_id": a.WriteID(),
				"created_at": a.CreatedAt(),
			}); err != nil {
			return fmt.Errorf("insert address: %w", err)
		}
		return nil
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
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockLiving(ctx, tx, a.AccountID()); err != nil {
			return err
		}
		if err := lockBook(ctx, tx, a.AccountID()); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, saveAddress, args)
		if err != nil {
			return fmt.Errorf("update address: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		// Setting the default leaves the row's write_id at writeID, which
		// a takes below.
		if err = clearDefault(ctx, tx, a.AccountID(), a.ID()); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE addresses SET is_default = true WHERE id = @id`, pgx.StrictNamedArgs{"id": a.ID()}); err != nil {
			return fmt.Errorf("set the default: %w", err)
		}
		return nil
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

func (s pgAddresses) SetDefault(ctx context.Context, owner, id uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockBook(ctx, tx, owner); err != nil {
			return err
		}
		var isDefault bool
		err := tx.QueryRow(ctx, `SELECT is_default FROM addresses WHERE id = @id AND account_id = @account`,
			pgx.StrictNamedArgs{"id": id, "account": owner}).Scan(&isDefault)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select address: %w", err)
		}
		if isDefault {
			return nil
		}
		if err = clearDefault(ctx, tx, owner, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE addresses SET is_default = true, write_id = uuidv7() WHERE id = @id`,
			pgx.StrictNamedArgs{"id": id}); err != nil {
			return fmt.Errorf("set the default: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set default address: %w", err)
	}
	return nil
}

func (s pgAddresses) Delete(ctx context.Context, owner, id uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockBook(ctx, tx, owner); err != nil {
			return err
		}
		var wasDefault bool
		err := tx.QueryRow(ctx, `DELETE FROM addresses WHERE id = @id AND account_id = @account RETURNING is_default`,
			pgx.StrictNamedArgs{"id": id, "account": owner}).Scan(&wasDefault)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("delete address: %w", err)
		}
		if !wasDefault {
			return nil
		}
		if _, err = tx.Exec(ctx, `UPDATE addresses SET is_default = true, write_id = uuidv7()
WHERE id = (SELECT id FROM addresses WHERE account_id = @account ORDER BY id DESC LIMIT 1)`,
			pgx.StrictNamedArgs{"account": owner}); err != nil {
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
