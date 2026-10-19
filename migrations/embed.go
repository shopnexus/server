// Package migrations holds the schema as numbered SQL files. A file that has
// run anywhere is never edited; fix mistakes with a new file.
package migrations

import "embed"

// FS holds every file in this directory. The pattern is "*" rather than
// "*.sql" so the package compiles before the first migration exists;
// postgres.LoadMigrations reads only NNNN_name.sql files.
//
//go:embed *
var FS embed.FS
