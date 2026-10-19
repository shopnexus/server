// Package postgres opens the pgx pool, runs transactions and migrations, and
// traces every query.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier runs SQL. *pgxpool.Pool and pgx.Tx both satisfy it, so a store
// method works inside and outside a transaction alike.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxRunner runs fn in a transaction: committed when fn returns nil, rolled
// back when it returns an error or panics.
type TxRunner interface {
	InTx(ctx context.Context, fn func(q Querier) error) error
}

// NewTxRunner returns a TxRunner on pool.
func NewTxRunner(pool *pgxpool.Pool) TxRunner { return txRunner{pool} }

type txRunner struct{ pool *pgxpool.Pool }

func (r txRunner) InTx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	// A no-op after Commit; runs on error and on panic.
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
