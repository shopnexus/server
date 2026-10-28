package migrations_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/migrations"
)

// before is the migrations up to, and not including, the file named last.
func before(t *testing.T, last string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	err := fs.WalkDir(migrations.FS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(name, ".sql") || name >= last {
			return err
		}
		b, readErr := fs.ReadFile(migrations.FS, name)
		out[name] = &fstest.MapFile{Data: b}
		return readErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// An account that signed in by email has a session and a push token; 0022
// deletes it with them, and keeps the phone of an account that had both.
func TestPhoneSignInDeletesEmailOnlyAccounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0022_phone_sign_in.sql")); err != nil {
		t.Fatal(err)
	}
	emailOnly, both := uuid.NewV7(), uuid.NewV7()
	for _, acc := range []uuid.UUID{emailOnly, both} {
		session := uuid.NewV7()
		args := pgx.NamedArgs{
			"acc": acc, "session": session, "email": acc.String() + "@example.com", "hash": session[:], "token": session.String(),
			"at": time.Now(),
		}
		for _, q := range []string{
			`INSERT INTO accounts (id, write_id) VALUES (@acc, @acc)`,
			`INSERT INTO credentials (account_id, write_id) VALUES (@acc, @acc)`,
			`INSERT INTO identities (id, account_id, kind, identifier) VALUES (@session, @acc, 'email', @email)`,
			`INSERT INTO sessions (id, account_id, refresh_hash, auth_time, created_at, last_used_at, expires_at, write_id)
			 VALUES (@session, @acc, @hash, @at, @at, @at, @at::timestamptz + interval '30 days', @session)`,
			`INSERT INTO push_tokens (session_id, account_id, token, updated_at) VALUES (@session, @acc, @token, now())`,
		} {
			if _, err := db.Exec(ctx, q, args); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO identities (id, account_id, kind, identifier) VALUES (gen_random_uuid(), @acc, 'phone', '+84901234567')`,
		pgx.StrictNamedArgs{"acc": both}); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("0022 on a database with an email-only account: %v", err)
	}
	var accounts, identities int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts), (SELECT count(*) FROM identities WHERE kind = 'phone')`).
		Scan(&accounts, &identities); err != nil || accounts != 1 || identities != 1 {
		t.Fatalf("accounts %d, phone identities %d: %v", accounts, identities, err)
	}
}
