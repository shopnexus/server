// Package repository stores module auth's entities in Postgres. Each method
// is its own transaction.
package repository

import "errors"

var (
	ErrNotFound = errors.New("auth: not found")
	ErrConflict = errors.New("auth: changed meanwhile")
)
