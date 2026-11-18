package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/notification/domain"
)

type pgSettings struct{ db *pgxpool.Pool }

func NewSettings(db *pgxpool.Pool) Settings { return pgSettings{db} }

func (s pgSettings) Get(ctx context.Context, account uuid.UUID) (domain.Settings, error) {
	var out domain.Settings
	err := s.db.QueryRow(ctx, `SELECT messages, offers, orders, price_cuts, followed, reviews, quiet_on, quiet_from, quiet_to
FROM notification_settings WHERE account_id = @account`, pgx.StrictNamedArgs{"account": account}).Scan(
		&out.Messages, &out.Offers, &out.Orders, &out.PriceCuts, &out.Followed, &out.Reviews,
		&out.Quiet.On, &out.Quiet.From, &out.Quiet.To)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultSettings(), nil
	}
	if err != nil {
		return domain.Settings{}, fmt.Errorf("read settings: %w", err)
	}
	return out, nil
}

// Put takes a share lock on the account's row, as every write that keeps a
// row of the caller does, so a PUT racing the account's deletion either
// lands before it, and the worker's Forget deletes it, or sees the
// tombstone.
func (s pgSettings) Put(ctx context.Context, account uuid.UUID, st domain.Settings) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var deleted bool
		err := tx.QueryRow(ctx, `SELECT status = 'deleted' FROM accounts WHERE id = @id FOR SHARE`,
			pgx.StrictNamedArgs{"id": account}).Scan(&deleted)
		if err != nil {
			return fmt.Errorf("lock account: %w", err)
		}
		if deleted {
			return accountdomain.ErrSessionRevoked
		}
		if _, err = tx.Exec(ctx, `INSERT INTO notification_settings
    (account_id, messages, offers, orders, price_cuts, followed, reviews, quiet_on, quiet_from, quiet_to)
VALUES (@account, @messages, @offers, @orders, @price_cuts, @followed, @reviews, @quiet_on, @quiet_from, @quiet_to)
ON CONFLICT (account_id) DO UPDATE SET messages = excluded.messages, offers = excluded.offers,
    orders = excluded.orders, price_cuts = excluded.price_cuts, followed = excluded.followed,
    reviews = excluded.reviews, quiet_on = excluded.quiet_on, quiet_from = excluded.quiet_from,
    quiet_to = excluded.quiet_to`, pgx.StrictNamedArgs{
			"account": account, "messages": st.Messages, "offers": st.Offers, "orders": st.Orders,
			"price_cuts": st.PriceCuts, "followed": st.Followed, "reviews": st.Reviews,
			"quiet_on": st.Quiet.On, "quiet_from": st.Quiet.From, "quiet_to": st.Quiet.To,
		}); err != nil {
			return fmt.Errorf("upsert settings: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("put settings: %w", err)
	}
	return nil
}
