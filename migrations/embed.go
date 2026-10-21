// Package migrations holds the schema as numbered SQL files. A file that has
// run anywhere is never edited; fix mistakes with a new file.
package migrations

import "embed"

// FS holds the migration files.
//
//go:embed *.sql
var FS embed.FS
