// Package repository stores module account's entities in Postgres. Each
// method is its own transaction.
package repository

import "errors"

var (
	ErrNotFound = errors.New("account: not found")
	ErrConflict = errors.New("account: changed meanwhile")
)
