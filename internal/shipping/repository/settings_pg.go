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
	b := &pgx.Batch{}
	postgres.QueueLockLiving(b, owner, accountdomain.ErrSessionRevoked)
	if st.Pickup == nil {
		b.Queue(`DELETE FROM shipping_settings WHERE account_id = @owner`, pgx.StrictNamedArgs{"owner": owner}).Fn = func(br pgx.BatchResults) error {
			if _, err := br.Exec(); err != nil {
				return fmt.Errorf("clear shipping settings: %w", err)
			}
			return nil
		}
	} else {
		// The address must be the owner's; its key share keeps it until commit.
		b.Queue(`INSERT INTO shipping_settings (account_id, address_id, phone)
SELECT @owner, id, @phone FROM addresses WHERE id = @address AND account_id = @owner FOR KEY SHARE
ON CONFLICT (account_id) DO UPDATE SET address_id = excluded.address_id, phone = excluded.phone
RETURNING account_id`, pgx.StrictNamedArgs{"owner": owner, "address": st.Pickup.AddressID, "phone": st.Pickup.Phone}).
			QueryRow(func(row pgx.Row) error {
				var id uuid.UUID
				err := row.Scan(&id)
				if errors.Is(err, pgx.ErrNoRows) {
					return accountdomain.ErrAddressNotFound
				}
				if err != nil {
					return fmt.Errorf("store shipping settings: %w", err)
				}
				return nil
			})
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("put shipping settings: %w", err)
	}
	return nil
}
