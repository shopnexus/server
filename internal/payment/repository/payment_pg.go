package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/platform/events"
)

type pgStore struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) Store { return pgStore{db} }

const columns = `id, account_id, product, product_id, amount, gateway, status, pay_url, gateway_ref, bank_code,
       result_code, created_at, expires_at, paid_at, closed_at, refunded_at, write_id`

func scan(row pgx.CollectableRow) (domain.Payment, error) {
	var st domain.State
	if err := row.Scan(&st.ID, &st.AccountID, &st.Product, &st.ProductID, &st.Amount, &st.Gateway, &st.Status, &st.PayURL,
		&st.GatewayRef, &st.BankCode, &st.ResultCode, &st.CreatedAt, &st.ExpiresAt, &st.PaidAt, &st.ClosedAt,
		&st.RefundedAt, &st.WriteID); err != nil {
		return domain.Payment{}, fmt.Errorf("scan payment: %w", err)
	}
	return domain.Restore(st), nil
}

func (s pgStore) Insert(ctx context.Context, p domain.Payment) (domain.Payment, error) {
	st := p.State()
	b := &pgx.Batch{}
	b.Queue(`INSERT INTO payments (id, account_id, product, product_id, amount, gateway, status, pay_url, created_at, expires_at, write_id)
VALUES (@id, @account, @product, @product_id, @amount, @gateway, @status, @pay_url, @created_at, @expires_at, @write_id)
ON CONFLICT (product, product_id) DO NOTHING`, pgx.StrictNamedArgs{
		"id": st.ID, "account": st.AccountID, "product": st.Product, "product_id": st.ProductID, "amount": st.Amount,
		"gateway": st.Gateway, "status": st.Status, "pay_url": st.PayURL, "created_at": st.CreatedAt,
		"expires_at": st.ExpiresAt, "write_id": st.WriteID,
	})
	var out domain.Payment
	b.Queue(`SELECT `+columns+` FROM payments WHERE product = @product AND product_id = @product_id`,
		pgx.StrictNamedArgs{"product": st.Product, "product_id": st.ProductID}).Query(func(rows pgx.Rows) error {
		var err error
		if out, err = pgx.CollectExactlyOneRow(rows, scan); err != nil {
			return fmt.Errorf("read payment: %w", err)
		}
		return nil
	})
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return domain.Payment{}, fmt.Errorf("insert payment: %w", err)
	}
	return out, nil
}

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (domain.Payment, error) {
	rows, err := s.db.Query(ctx, `SELECT `+columns+` FROM payments WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return domain.Payment{}, fmt.Errorf("select payment: %w", err)
	}
	p, err := pgx.CollectExactlyOneRow(rows, scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Payment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, fmt.Errorf("read payment: %w", err)
	}
	return p, nil
}

func (s pgStore) GetMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Payment, error) {
	out := map[uuid.UUID]domain.Payment{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `SELECT `+columns+` FROM payments WHERE id = ANY(@ids)`, pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select payments: %w", err)
	}
	ps, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("read payments: %w", err)
	}
	for i := range ps {
		out[ps[i].ID()] = ps[i]
	}
	return out, nil
}

func (s pgStore) Save(ctx context.Context, p *domain.Payment, writeID uuid.UUID, evs ...events.Event) error {
	st := p.State()
	b := &pgx.Batch{}
	b.Queue(`UPDATE payments SET status = @status, gateway_ref = @gateway_ref, bank_code = @bank_code, result_code = @result_code,
    paid_at = @paid_at, closed_at = @closed_at, refunded_at = @refunded_at, write_id = @write_id
WHERE id = @id AND write_id = @read_write_id`, pgx.StrictNamedArgs{
		"id": st.ID, "status": st.Status, "gateway_ref": st.GatewayRef, "bank_code": st.BankCode, "result_code": st.ResultCode,
		"paid_at": st.PaidAt, "closed_at": st.ClosedAt, "refunded_at": st.RefundedAt, "write_id": writeID, "read_write_id": st.WriteID,
	}).Exec(func(tag pgconn.CommandTag) error {
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		return nil
	})
	events.Queue(b, evs...)
	if err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error { return tx.SendBatch(ctx, b).Close() }); err != nil {
		return fmt.Errorf("save payment: %w", err)
	}
	p.Saved(writeID)
	return nil
}
