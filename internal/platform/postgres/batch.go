package postgres

import (
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ExecAs is a QueuedQuery.Fn that names a failed statement by what. A
// non-nil none is the error when the statement touches no row; it aborts
// only a batch sent in a tx.
func ExecAs(what string, none error) func(pgx.BatchResults) error {
	return func(br pgx.BatchResults) error {
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
