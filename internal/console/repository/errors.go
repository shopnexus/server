// Package repository stores module console's members, sessions and sign-in
// locks in Postgres.
package repository

import "errors"

var (
	ErrNotFound = errors.New("console: not found")
	ErrConflict = errors.New("console: changed meanwhile")
)
