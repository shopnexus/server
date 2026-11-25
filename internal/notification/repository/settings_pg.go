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
	"github.com/shopnexus/server/internal/platform/postgres"
)

type pgSettings struct{ db *pgxpool.Pool }

func NewSettings(db *pgxpool.Pool) Settings { return pgSettings{db} }

func (s pgSettings) Get(ctx context.Context, account uuid.UUID) (domain.Settings, error) {
	var out domain.Settings
	err := s.db.QueryRow(ctx, `SELECT messages, offers, orders, price_cuts, followed, reviews, order_emails, quiet_on, quiet_from, quiet_to
FROM notification_settings WHERE account_id = @account`, pgx.StrictNamedArgs{"account": account}).Scan(
		&out.Messages, &out.Offers, &out.Orders, &out.PriceCuts, &out.Followed, &out.Reviews, &out.OrderEmails,
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
	b := &pgx.Batch{}
	postgres.QueueLockLiving(b, account, accountdomain.ErrSessionRevoked)
	b.Queue(`INSERT INTO notification_settings
    (account_id, messages, offers, orders, price_cuts, followed, reviews, order_emails, quiet_on, quiet_from, quiet_to)
VALUES (@account, @messages, @offers, @orders, @price_cuts, @followed, @reviews, @order_emails, @quiet_on, @quiet_from, @quiet_to)
ON CONFLICT (account_id) DO UPDATE SET messages = excluded.messages, offers = excluded.offers,
    orders = excluded.orders, price_cuts = excluded.price_cuts, followed = excluded.followed,
    reviews = excluded.reviews, order_emails = excluded.order_emails, quiet_on = excluded.quiet_on, quiet_from = excluded.quiet_from,
    quiet_to = excluded.quiet_to`, pgx.StrictNamedArgs{
		"account": account, "messages": st.Messages, "offers": st.Offers, "orders": st.Orders,
		"price_cuts": st.PriceCuts, "followed": st.Followed, "reviews": st.Reviews, "order_emails": st.OrderEmails,
		"quiet_on": st.Quiet.On, "quiet_from": st.Quiet.From, "quiet_to": st.Quiet.To,
	})
	// QueueLockLiving's callback aborts, so the batch needs a transaction.
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("put settings: %w", err)
	}
	return nil
}
