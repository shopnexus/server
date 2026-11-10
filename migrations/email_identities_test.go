package migrations_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// After 0029 an account holds a phone, an email or both, one of each kind,
// and a code challenge is an email's.
func TestEmailIdentities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	acc := uuid.NewV7()
	exec := func(q string, args pgx.StrictNamedArgs) error {
		_, err := db.Exec(ctx, q, args)
		return err
	}
	identity := func(kind, value string) error {
		return exec(`INSERT INTO identities (id, account_id, kind, identifier) VALUES (@id, @acc, @kind, @value)`,
			pgx.StrictNamedArgs{"id": uuid.NewV7(), "acc": acc, "kind": kind, "value": value})
	}
	code := func(err error) string {
		if pg, ok := errors.AsType[*pgconn.PgError](err); ok {
			return pg.Code
		}
		return ""
	}
	for _, q := range []string{
		`INSERT INTO accounts (id, write_id) VALUES (@acc, @acc)`,
		`INSERT INTO credentials (account_id, write_id) VALUES (@acc, @acc)`,
	} {
		if err := exec(q, pgx.StrictNamedArgs{"acc": acc}); err != nil {
			t.Fatal(err)
		}
	}
	if err := identity("phone", "+84901234567"); err != nil {
		t.Fatalf("phone: %v", err)
	}
	if err := identity("email", "linh@example.com"); err != nil {
		t.Fatalf("email beside the phone: %v", err)
	}
	if err := identity("email", "linh2@example.com"); code(err) != "23505" {
		t.Fatalf("a second email: %v", err)
	}
	if err := identity("google", "linh@gmail.com"); code(err) != "23514" {
		t.Fatalf("kind google: %v", err)
	}
	challenge := `INSERT INTO otp_challenges (id, purpose, kind, destination, account_id, code_hash, ip, expires_at, created_at, write_id)
VALUES (@id, @purpose, @kind, 'linh@example.com', @acc, '\x00', '127.0.0.1', @at, @at, @id)`
	at := time.Now()
	for _, c := range []struct {
		name, purpose, kind string
		acc                 *uuid.UUID
		want                string
	}{
		{"sign-in", "sign_in", "email", nil, ""},
		{"add identity", "add_identity", "email", &acc, ""},
		{"add identity without an account", "add_identity", "email", nil, "23514"},
		{"sign-in naming an account", "sign_in", "email", &acc, "23514"},
		{"a phone", "sign_in", "phone", nil, "23514"},
	} {
		id := uuid.NewV7()
		err := exec(challenge, pgx.StrictNamedArgs{"id": id, "purpose": c.purpose, "kind": c.kind, "acc": c.acc, "at": at})
		if code(err) != c.want {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
