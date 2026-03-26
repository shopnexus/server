package pgsqlc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TxBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

type Querier = any

func NewStorage[T Querier](conn TxBeginner, querier T) *storage[T] {
	return &storage[T]{
		queries:       querier,
		conn:          conn,
		allowNestedTx: false,
	}
}

type Storage[T Querier] interface {
	// Conn returns the current connection
	Conn() TxBeginner
	// Querier returns the querier instance
	Querier() T
	// BeginTx starts a new transaction
	BeginTx(ctx context.Context) (*TxStorage[T], error)
}

// Storage provides database queries with transaction support
type storage[T Querier] struct {
	queries       T
	conn          TxBeginner
	allowNestedTx bool
}

func (s *storage[T]) Querier() T {
	return s.queries
}

func (s *storage[T]) Conn() TxBeginner {
	return s.conn
}

// BeginTx starts a new database transaction
func (s *storage[T]) BeginTx(ctx context.Context) (*TxStorage[T], error) {
	var tx pgx.Tx
	var err error

	tx, err = s.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	queriesWithTx, ok := any(s.queries).(interface {
		WithTx(tx pgx.Tx) T
	})
	if !ok {
		return nil, fmt.Errorf("queries does not implement WithTx method")
	}

	return &TxStorage[T]{
		tx: tx,
		storage: &storage[T]{
			queries:       queriesWithTx.WithTx(tx),
			conn:          tx,
			allowNestedTx: s.allowNestedTx,
		},
	}, nil
}

// TxStorage provides database queries within an active transaction
type TxStorage[T Querier] struct {
	tx        pgx.Tx
	committed bool
	*storage[T]
}

func (ts *TxStorage[T]) Commit(ctx context.Context) error {
	if err := ts.tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transactional queries: %w", err)
	}
	ts.committed = true
	return nil
}

func (ts *TxStorage[T]) Rollback(ctx context.Context) error {
	// if ts.committed {
	// 	return nil
	// }
	if err := ts.tx.Rollback(ctx); !errors.Is(err, pgx.ErrTxClosed) && err != nil {
		slog.Error("failed to rollback transaction", slog.Any("error", err))
		return fmt.Errorf("failed to rollback transactional queries: %w", err)
	}
	return nil
}
