package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/shipping/domain"
)

type Settings interface {
	Get(ctx context.Context, owner uuid.UUID) (domain.Settings, error)
	Put(ctx context.Context, owner uuid.UUID, settings domain.Settings) error
}

type pgSettings struct{ db *pgxpool.Pool }

func NewSettings(db *pgxpool.Pool) Settings { return pgSettings{db} }

func (s pgSettings) Get(ctx context.Context, owner uuid.UUID) (domain.Settings, error) {
	var p domain.SavedPickup
	err := s.db.QueryRow(ctx, `SELECT address_id, phone FROM shipping_settings WHERE account_id = @owner`,
		pgx.StrictNamedArgs{"owner": owner}).Scan(&p.AddressID, &p.Phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Settings{}, nil
	}
	if err != nil {
		return domain.Settings{}, fmt.Errorf("read shipping settings: %w", err)
	}
	return domain.Settings{Pickup: &p}, nil
}

func (s pgSettings) Put(ctx context.Context, owner uuid.UUID, st domain.Settings) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		living, err := postgres.LockLiving(ctx, tx, owner)
		if err != nil {
			return err
		}
		if !living {
			return accountdomain.ErrSessionRevoked
		}
		if st.Pickup == nil {
			_, err = tx.Exec(ctx, `DELETE FROM shipping_settings WHERE account_id = @owner`, pgx.StrictNamedArgs{"owner": owner})
			if err != nil {
				return fmt.Errorf("clear shipping settings: %w", err)
			}
			return nil
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM addresses WHERE id = @address AND account_id = @owner FOR KEY SHARE`,
			pgx.StrictNamedArgs{"address": st.Pickup.AddressID, "owner": owner}).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return accountdomain.ErrAddressNotFound
		}
		if err != nil {
			return fmt.Errorf("lock pickup address: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO shipping_settings (account_id, address_id, phone) VALUES (@owner, @address, @phone)
ON CONFLICT (account_id) DO UPDATE SET address_id = excluded.address_id, phone = excluded.phone`,
			pgx.StrictNamedArgs{"owner": owner, "address": id, "phone": st.Pickup.Phone})
		if err != nil {
			return fmt.Errorf("store shipping settings: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("put shipping settings: %w", err)
	}
	return nil
}
