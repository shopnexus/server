package pgsqlc

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/logger"
)

type TxBeginner interface {
	db.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

func NewTxQueries(conn TxBeginner, allowNestedTx bool) *storage {
	return &storage{
		conn:          conn,
		Queries:       db.New(conn),
		allowNestedTx: allowNestedTx,
	}
}

type Storage interface {
	db.Querier
	// BeginTx starts a new transaction, prefer using the provided Storage if not nil, mannually manage the transaction
	BeginTx(ctx context.Context, preferStorage Storage) (*TxStorage, error)
	// WithTx executes the given function within a transaction, prefer using the provided Storage if not nil, automatically commit/rollback
	WithTx(ctx context.Context, preferStorage Storage, fn func(txStorage Storage) error) error
}

// Storage provides database queries with transaction support
type storage struct {
	conn TxBeginner
	*db.Queries
	allowNestedTx bool
}

// BeginTx starts a new database transaction
func (s *storage) BeginTx(ctx context.Context, preferStorage Storage) (*TxStorage, error) {
	if preferStorage != nil && !s.allowNestedTx {
		// if preferStorage is already a TxStorage and not
		if _, ok := preferStorage.(*TxStorage); ok {
			return preferStorage.(*TxStorage), nil
		}

		// begin a new transaction
		return preferStorage.BeginTx(ctx, nil)
	}

	var tx pgx.Tx
	var err error

	// begin a nested transaction using savepoint
	tx, err = s.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return &TxStorage{
		tx: tx,
		storage: &storage{
			conn:          tx,
			Queries:       db.New(tx),
			allowNestedTx: s.allowNestedTx,
		},
	}, nil
}

func (s *storage) WithTx(ctx context.Context, preferStorage Storage, fn func(txStorage Storage) error) error {
	txStorage, err := s.BeginTx(ctx, preferStorage)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer txStorage.Rollback(ctx)

	if err := fn(txStorage); err != nil {
		return fmt.Errorf("failed to execute transaction: %w", err)
	}

	return txStorage.Commit(ctx)
}

// TxStorage provides database queries within an active transaction
type TxStorage struct {
	tx pgx.Tx
	*storage
}

func (ts *TxStorage) Commit(ctx context.Context) error {
	if err := ts.tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transactional queries: %w", err)
	}
	return nil
}

func (ts *TxStorage) Rollback(ctx context.Context) error {
	if err := ts.tx.Rollback(ctx); !errors.Is(err, pgx.ErrTxClosed) && err != nil {
		// TODO: push to error tracking system
		logger.Log.Sugar().Errorf("failed to rollback transaction: %v", err)
		return fmt.Errorf("failed to rollback transactional queries: %w", err)
	}
	return nil
}
