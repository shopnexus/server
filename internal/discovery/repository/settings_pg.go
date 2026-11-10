package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
)

type pgSettings struct{ db *pgxpool.Pool }

func NewSettings(db *pgxpool.Pool) Settings { return pgSettings{db} }

func (s pgSettings) PriceCutAlerts(ctx context.Context, account uuid.UUID) (bool, error) {
	var on bool
	err := s.db.QueryRow(ctx, `SELECT price_cut_alerts FROM saved_settings WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read settings: %w", err)
	}
	return on, nil
}

func (s pgSettings) SetPriceCutAlerts(ctx context.Context, account uuid.UUID, on bool) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		alive, err := living(ctx, tx, account)
		if err != nil {
			return err
		}
		if !alive {
			return accountdomain.ErrSessionRevoked
		}
		if _, err := tx.Exec(ctx, `INSERT INTO saved_settings (account_id, price_cut_alerts) VALUES (@account, @on)
ON CONFLICT (account_id) DO UPDATE SET price_cut_alerts = excluded.price_cut_alerts`,
			pgx.StrictNamedArgs{"account": account, "on": on}); err != nil {
			return fmt.Errorf("upsert settings: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set price cut alerts: %w", err)
	}
	return nil
}

func (s pgSettings) Clear(ctx context.Context, account uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM saved_settings WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": account}); err != nil {
		return fmt.Errorf("clear settings: %w", err)
	}
	return nil
}
