package migrations_test

import (
	"context"
	"strings"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// The checks hold a draft to the API's shape.
func TestListingDraftsChecks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	acc := accounttest.New(t, db)
	insert := func(schemaVersion int, title string, photos int, price int64) error {
		ids := make([]uuid.UUID, photos)
		for i := range ids {
			ids[i] = uuid.NewV7()
		}
		_, err := db.Exec(ctx, `INSERT INTO listing_drafts
    (id, account_id, schema_version, title, price, photos, body, created_at, updated_at, expires_at)
VALUES (@id, @acc, @schema_version, @title, @price, @photos, '{}', now(), statement_timestamp(), now() + interval '30 days')`,
			pgx.StrictNamedArgs{"id": uuid.NewV7(), "acc": acc, "schema_version": schemaVersion, "title": title, "photos": ids, "price": price})
		return err
	}
	if err := insert(1, strings.Repeat("ă", 100), 10, 0); err != nil {
		t.Fatalf("a valid draft: %v", err)
	}
	for name, err := range map[string]error{
		"schema version 0":   insert(0, "", 0, 0),
		"a 101-letter title": insert(1, strings.Repeat("ă", 101), 0, 0),
		"11 photos":          insert(1, "", 11, 0),
		"a negative price":   insert(1, "", 0, -1),
	} {
		if err == nil {
			t.Errorf("%s was stored", name)
		}
	}
}
