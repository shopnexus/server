// Package repository stores module account's entities in Postgres. Each
// method is its own transaction.
package repository

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound = errors.New("account: not found")
	ErrConflict = errors.New("account: changed meanwhile")
)

// queue adds sql to b; its failure names what. A non-nil none is the
// error when the statement touches no row, so the batch must run in a tx.
func queue(b *pgx.Batch, what, sql string, args pgx.StrictNamedArgs, none error) {
	b.Queue(sql, args).Fn = func(br pgx.BatchResults) error {
		tag, err := br.Exec()
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if none != nil && tag.RowsAffected() == 0 {
			return none
		}
		return nil
	}
}
