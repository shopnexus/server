package migrations_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/migrations"
)

// An upload that 0041 finds expires a day after it was created, as before;
// one the release before inserts without expires_at gets a day from now.
func TestUploadExpiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0041_upload_expiry.sql")); err != nil {
		t.Fatal(err)
	}
	acc := accounttest.New(t, db)
	id := uuid.NewV7()
	created := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	if _, err := db.Exec(ctx, `INSERT INTO uploads (id, owner_id, content_type, size, status, write_id, created_at, updated_at)
VALUES (@id, @owner, 'image/jpeg', 10, 'waiting', gen_random_uuid(), @created, @created)`,
		pgx.StrictNamedArgs{"id": id, "owner": acc, "created": created}); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("0041 on a database with uploads: %v", err)
	}
	var expires time.Time
	if err := db.QueryRow(ctx, `SELECT expires_at FROM uploads WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).Scan(&expires); err != nil ||
		!expires.Equal(created.Add(24*time.Hour)) {
		t.Fatalf("an upload 0041 found: expires %v, %v", expires, err)
	}
	var ahead int64
	if err := db.QueryRow(ctx, `INSERT INTO uploads (id, owner_id, content_type, size, status, write_id, created_at, updated_at)
VALUES (gen_random_uuid(), @owner, 'image/jpeg', 10, 'waiting', gen_random_uuid(), now(), now())
RETURNING extract(epoch FROM expires_at - now())::bigint`, pgx.StrictNamedArgs{"owner": acc}).Scan(&ahead); err != nil || ahead != 86400 {
		t.Fatalf("an upload the release before inserts: expires in %d s, %v", ahead, err)
	}
}
