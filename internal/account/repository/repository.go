// Package repository stores module account's entities in Postgres. Each
// method is its own transaction.
package repository

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/postgres"
)

var (
	ErrNotFound = errors.New("account: not found")
	ErrConflict = errors.New("account: changed meanwhile")
)

// queue adds sql to b; its failure names what. A non-nil none is the
// error when the statement touches no row, so the batch must run in a tx.
func queue(b *pgx.Batch, what, sql string, args pgx.StrictNamedArgs, none error) {
	b.Queue(sql, args).Fn = postgres.ExecAs(what, none)
}
