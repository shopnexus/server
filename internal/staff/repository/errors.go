// Package repository stores module staff's members, sessions and sign-in
// locks in Postgres.
package repository

import "errors"

var (
	ErrNotFound = errors.New("staff: not found")
	ErrConflict = errors.New("staff: changed meanwhile")
)
