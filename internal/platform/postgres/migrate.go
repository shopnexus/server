package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrChecksumMismatch means a migration file changed after it was applied.
// Fix forward with a new migration instead.
var ErrChecksumMismatch = errors.New("applied migration was modified")

// Migration is one numbered SQL file.
type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum [32]byte
}

// File is the migration's file name, as people see it.
func (m Migration) File() string { return fmt.Sprintf("%04d_%s.sql", m.Version, m.Name) }

// migrateLock is the advisory lock key that keeps two processes from
// migrating one database at once.
const migrateLock = 7_301_001

var fileName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// LoadMigrations reads every NNNN_name.sql file at the root of fsys, sorted by
// version. Other files are ignored.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var migs []Migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m := fileName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration %s: name must look like 0001_create_things.sql", e.Name())
		}
		version, _ := strconv.Atoi(m[1])
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations %s and %s share version %d", prev, e.Name(), version)
		}
		seen[version] = e.Name()
		body, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		migs = append(migs, Migration{Version: version, Name: m[2], SQL: string(body), Checksum: sha256.Sum256(body)})
	}
	slices.SortFunc(migs, func(a, b Migration) int { return a.Version - b.Version })
	return migs, nil
}

const createTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version    int PRIMARY KEY,
	name       text NOT NULL,
	checksum   bytea NOT NULL,
	applied_at timestamptz NOT NULL DEFAULT now()
)`

// Migrate applies the pending migrations in fsys, each in its own transaction,
// and returns the ones it applied. It stops with ErrChecksumMismatch when an
// applied file changed.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]Migration, error) {
	migs, err := LoadMigrations(fsys)
	if err != nil {
		return nil, err
	}

	// A session-level advisory lock belongs to one connection, so lock,
	// migrate and unlock on the same acquired connection.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLock); err != nil {
		return nil, err
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrateLock)

	if _, err := conn.Exec(ctx, createTable); err != nil {
		return nil, err
	}
	pending, err := pending(ctx, conn.Conn(), migs)
	if err != nil {
		return nil, err
	}
	for i, m := range pending {
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.SQL); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
				m.Version, m.Name, m.Checksum[:])
			return err
		})
		if err != nil {
			return pending[:i], fmt.Errorf("migration %s: %w", m.File(), err)
		}
	}
	return pending, nil
}

// Pending returns the migrations in fsys that the database has not applied.
func Pending(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]Migration, error) {
	migs, err := LoadMigrations(fsys)
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return migs, nil
	}
	return pending(ctx, conn.Conn(), migs)
}

func pending(ctx context.Context, conn *pgx.Conn, migs []Migration) ([]Migration, error) {
	rows, err := conn.Query(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	applied := map[int][]byte{}
	for rows.Next() {
		var v int
		var sum []byte
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		applied[v] = sum
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []Migration
	for _, m := range migs {
		sum, ok := applied[m.Version]
		if !ok {
			out = append(out, m)
			continue
		}
		if string(sum) != string(m.Checksum[:]) {
			return nil, fmt.Errorf("%w: %s", ErrChecksumMismatch, m.File())
		}
	}
	return out, nil
}
