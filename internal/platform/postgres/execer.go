package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is a pool or a transaction, for a statement that runs alone or
// inside a repository's transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
