package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/domain"
)

// lockBook takes owner's advisory lock until tx ends.
func lockBook(ctx context.Context, tx pgx.Tx, owner uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(@account::text))`,
		pgx.StrictNamedArgs{"account": owner}); err != nil {
		return fmt.Errorf("lock account: %w", err)
	}
	return nil
}

func (s pgAccounts) InsertAddress(ctx context.Context, key uuid.UUID, a *domain.Address) error {
	var (
		first  bool
		stored *domain.Address
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := lockBook(ctx, tx, a.AccountID()); err != nil {
			return err
		}
		var (
			id, writeID uuid.UUID
			d           domain.AddressDraft
			isDefault   bool
			createdAt   time.Time
		)
		err := tx.QueryRow(ctx, `SELECT id, recipient_name, phone, street, commune_code, is_default, write_id, created_at
FROM addresses WHERE account_id = @account AND request_key = @key`,
			pgx.StrictNamedArgs{"account": a.AccountID(), "key": key}).
			Scan(&id, &d.Name, &d.Phone, &d.Street, &d.CommuneCode, &isDefault, &writeID, &createdAt)
		if err == nil {
			stored = new(domain.RestoreAddress(id, a.AccountID(), d, isDefault, writeID, createdAt))
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
		first = n == 0
		if _, err := tx.Exec(ctx, `INSERT INTO addresses
    (id, account_id, recipient_name, phone, street, commune_code, is_default, request_key, write_id, created_at)
VALUES (@id, @account, @name, @phone, @street, @commune_code, @is_default, @key, @write_id, @created_at)`,
			pgx.StrictNamedArgs{
				"id": a.ID(), "account": a.AccountID(), "name": a.Name(), "phone": a.Phone(), "street": a.Street(),
				"commune_code": a.CommuneCode(), "is_default": first, "key": key, "write_id": a.WriteID(),
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
	if first {
		a.StoredAsDefault()
	}
	return nil
}

func (s pgAccounts) GetAddress(ctx context.Context, owner, id uuid.UUID) (domain.Address, error) {
	var (
		d         domain.AddressDraft
		isDefault bool
		writeID   uuid.UUID
		createdAt time.Time
	)
	err := s.db.QueryRow(ctx, `SELECT recipient_name, phone, street, commune_code, is_default, write_id, created_at
FROM addresses WHERE id = @id AND account_id = @account`, pgx.StrictNamedArgs{"id": id, "account": owner}).
		Scan(&d.Name, &d.Phone, &d.Street, &d.CommuneCode, &isDefault, &writeID, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Address{}, ErrNotFound
	}
	if err != nil {
		return domain.Address{}, fmt.Errorf("select address: %w", err)
	}
	return domain.RestoreAddress(id, owner, d, isDefault, writeID, createdAt), nil
}

func (s pgAccounts) SaveAddress(ctx context.Context, a *domain.Address, writeID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `UPDATE addresses
SET recipient_name = @name, phone = @phone, street = @street, commune_code = @commune_code, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`, pgx.StrictNamedArgs{
		"id": a.ID(), "read_write_id": a.WriteID(), "write_id": writeID,
		"name": a.Name(), "phone": a.Phone(), "street": a.Street(), "commune_code": a.CommuneCode(),
	})
	if err != nil {
		return fmt.Errorf("update address: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	a.Saved(writeID)
	return nil
}

func (s pgAccounts) SetDefaultAddress(ctx context.Context, owner, id uuid.UUID) error {
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
		// The old default goes first: the unique index on the default is
		// checked row by row.
		if _, err = tx.Exec(ctx, `UPDATE addresses SET is_default = false, write_id = uuidv7()
WHERE account_id = @account AND is_default`, pgx.StrictNamedArgs{"account": owner}); err != nil {
			return fmt.Errorf("clear the default: %w", err)
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

func (s pgAccounts) DeleteAddress(ctx context.Context, owner, id uuid.UUID) error {
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

func (s pgAccounts) ListAddresses(ctx context.Context, owner uuid.UUID) ([]AddressRow, error) {
	rows, err := s.db.Query(ctx, `SELECT a.id, a.recipient_name, a.phone, a.street, a.commune_code, c.display_name,
       p.display_name, a.is_default, a.created_at
FROM addresses a JOIN communes c ON c.code = a.commune_code JOIN provinces p ON p.code = c.province_code
WHERE a.account_id = @account ORDER BY a.is_default DESC, a.id DESC`, pgx.StrictNamedArgs{"account": owner})
	if err != nil {
		return nil, fmt.Errorf("select addresses: %w", err)
	}
	defer rows.Close()
	var out []AddressRow
	for rows.Next() {
		var r AddressRow
		if err = rows.Scan(&r.ID, &r.Name, &r.Phone, &r.Street, &r.CommuneCode, &r.Commune, &r.Province,
			&r.IsDefault, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan address: %w", err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read addresses: %w", err)
	}
	return out, nil
}

func (s pgAccounts) Commune(ctx context.Context, code string) (Place, error) {
	var p Place
	err := s.db.QueryRow(ctx, `SELECT c.display_name, p.display_name
FROM communes c JOIN provinces p ON p.code = c.province_code WHERE c.code = @code`, pgx.StrictNamedArgs{"code": code}).
		Scan(&p.Commune, &p.Province)
	if errors.Is(err, pgx.ErrNoRows) {
		return Place{}, ErrNotFound
	}
	if err != nil {
		return Place{}, fmt.Errorf("select commune: %w", err)
	}
	return p, nil
}
