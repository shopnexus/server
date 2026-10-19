package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// UniqueViolation reports whether err is a unique-constraint violation
// (SQLSTATE 23505) and which constraint it hit. Stores turn it into a
// business error; the constraint name never reaches a client.
func UniqueViolation(err error) (constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName, true
	}
	return "", false
}
