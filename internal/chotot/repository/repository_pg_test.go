package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/chotot/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

var now = time.Now().UTC().Truncate(time.Microsecond)

// oid is a profile ID of its own for each call.
func oid() string {
	id := uuid.NewV7()
	return id.String()[0:8] + id.String()[9:13] + id.String()[14:18] + id.String()[19:23] + id.String()[24:]
}

func count(t *testing.T, db *pgxpool.Pool, query string, args pgx.StrictNamedArgs) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCodes(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	codes := repository.NewCodes(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	if _, found, err := codes.Get(ctx, me); err != nil || found {
		t.Fatalf("none: %v %v", found, err)
	}
	first := domain.NewCode(now)
	got, err := codes.Issue(ctx, me, first, now)
	if err != nil || got.Code != first.Code || !got.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("issue: %+v %v", got, err)
	}
	if got, _ = codes.Issue(ctx, me, domain.NewCode(now), now.Add(time.Minute)); got.Code != first.Code {
		t.Fatalf("a live code was replaced: %+v", got)
	}
	later := now.Add(domain.CodeLife)
	second := domain.NewCode(later)
	if got, _ = codes.Issue(ctx, me, second, later); got.Code != second.Code {
		t.Fatalf("an expired code stayed: %+v", got)
	}
	if c, found, _ := codes.Get(ctx, me); !found || c.Code != second.Code {
		t.Fatalf("get: %+v", c)
	}
	gone := accounttest.New(t, db)
	if _, err = db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = $1`, gone); err != nil {
		t.Fatal(err)
	}
	if _, err = codes.Issue(ctx, gone, first, now); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("a deleted account: %v", err)
	}
}

// link issues a code for account and links profile with it.
func link(t *testing.T, db *pgxpool.Pool, account uuid.UUID, profile string) (domain.Link, error) {
	t.Helper()
	ctx := context.Background()
	c, err := repository.NewCodes(db).Issue(ctx, account, domain.NewCode(now), now)
	if err != nil {
		t.Fatal(err)
	}
	seen, waiting := int64(100), 3
	l := domain.NewLink(uuid.NewV7(), account, profile, 42, "Na", "https://cdn.chotot.com/a.jpg", now)
	l.SeenThrough, l.Waiting = &seen, &waiting
	return repository.NewLinks(db).Link(ctx, l, c.Code)
}

func TestLinks(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	links := repository.NewLinks(db)
	ctx := context.Background()
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	profile := oid()
	first, err := link(t, db, me, profile)
	if err != nil || !first.AutoImport || *first.SeenThrough != 100 {
		t.Fatalf("link: %+v %v", first, err)
	}
	if _, found, _ := repository.NewCodes(db).Get(ctx, me); found {
		t.Fatal("the code was not used up")
	}
	if _, err = links.Link(ctx, domain.NewLink(uuid.NewV7(), me, profile, 42, "Na", "", now), "SN-AAAAAA"); !errors.Is(err, domain.ErrCodeExpired) {
		t.Fatalf("a used code: %v", err)
	}
	off := false
	if l, found, updateErr := links.Update(ctx, me, domain.SettingsPatch{AutoImport: &off}); updateErr != nil || !found || l.AutoImport || !l.RemindHide {
		t.Fatalf("update: %+v %v %v", l, found, updateErr)
	}
	if _, found, _ := links.Update(ctx, other, domain.SettingsPatch{AutoImport: &off}); found {
		t.Fatal("update without a link")
	}
	again, err := link(t, db, me, profile)
	if err != nil || again.ID == first.ID || again.AutoImport {
		t.Fatalf("linking again lost the settings: %+v %v", again, err)
	}
	if _, found, _ := links.ByID(ctx, first.ID); found {
		t.Fatal("the old link stayed")
	}
	// The profile moves to another account, which proved it holds it.
	moved, err := link(t, db, other, profile)
	if err != nil || !moved.AutoImport {
		t.Fatalf("move: %+v %v", moved, err)
	}
	if _, found, _ := links.Get(ctx, me); found {
		t.Fatal("the first account kept the profile")
	}
	if got, found, _ := links.Get(ctx, other); !found || got.ID != moved.ID || got.Name != "Na" || got.CheckedAt != nil {
		t.Fatalf("get: %+v", got)
	}
	if err = links.Checked(ctx, moved.ID, now, 5, 90); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := links.ByID(ctx, moved.ID); got.CheckedAt == nil || *got.Waiting != 5 || *got.SeenThrough != 100 {
		t.Fatalf("checked: %+v", got)
	}
	ev := uuid.NewV7()
	if err = links.Imported(ctx, moved.ID, 2, ev, now); err != nil {
		t.Fatal(err)
	}
	if err = links.Imported(ctx, first.ID, 2, uuid.NewV7(), now); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM events WHERE kind = 'chotot_link.imported' AND subject_id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": []uuid.UUID{moved.ID, first.ID}}); n != 1 {
		t.Fatalf("%d events", n)
	}
	if err = links.Delete(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := links.Get(ctx, other); found {
		t.Fatal("unlink")
	}
}

func TestImports(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	imports := repository.NewImports(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	profile := oid()
	a, b := domain.NewImport(uuid.NewV7(), me, 11, profile, now), domain.NewImport(uuid.NewV7(), me, 12, profile, now)
	got, err := imports.Start(ctx, []domain.Import{a, b}, now)
	if err != nil || len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("start: %+v %v", got, err)
	}
	// A running import answers as it is.
	if got, _ = imports.Start(ctx, []domain.Import{domain.NewImport(uuid.NewV7(), me, 11, profile, now)}, now); got[0].ID != a.ID {
		t.Fatalf("a running import restarted: %+v", got)
	}
	listing := catalogtest.Listing(t, db, me)
	a.Listed(listing, []string{domain.FieldCondition}, 1_500_000, now)
	if ok, saveErr := imports.Save(ctx, a, domain.Importing); saveErr != nil || !ok {
		t.Fatalf("save: %v %v", ok, saveErr)
	}
	if ok, _ := imports.Save(ctx, a, domain.Importing); ok {
		t.Fatal("saved twice from importing")
	}
	b.Ended(domain.Skipped, "listing_cap_reached", now)
	if ok, _ := imports.Save(ctx, b, domain.Importing); !ok {
		t.Fatal("skip")
	}
	// A skipped import starts again under a new ID; a listed one stays.
	c := domain.NewImport(uuid.NewV7(), me, 12, profile, now)
	got, err = imports.Start(ctx, []domain.Import{domain.NewImport(uuid.NewV7(), me, 11, profile, now), c}, now)
	if err != nil || got[0].ID != a.ID || got[0].State != domain.Listed || got[1].ID != c.ID || got[1].State != domain.Importing {
		t.Fatalf("restart: %+v %v", got, err)
	}
	if ok, _ := imports.Save(ctx, b, domain.Skipped); ok {
		t.Fatal("the old run saved over the new one")
	}
	c.Ended(domain.Skipped, "ad_gone", now)
	price := int64(9)
	c.Drafted(domain.Draft{Title: "Máy", Price: &price, Photos: []uuid.UUID{uuid.NewV7()}}, domainErr(), 9, now)
	if ok, _ := imports.Save(ctx, c, domain.Importing); !ok {
		t.Fatal("draft")
	}
	if i, found, _ := imports.Get(ctx, me, 12); !found || i.Draft == nil || *i.Draft.Price != 9 || i.Review[0] != "price" {
		t.Fatalf("get draft: %+v", i)
	}
	if i, found, _ := imports.ByListing(ctx, listing); !found || i.ID != a.ID || i.Review[0] != "condition" {
		t.Fatalf("by listing: %+v", i)
	}
	if l, _ := imports.List(ctx, me); len(l) != 2 || l[0].ListID != 12 {
		t.Fatalf("list: %+v", l)
	}
	if err = imports.ClearReview(ctx, listing); err != nil {
		t.Fatal(err)
	}
	if err = imports.Followed(ctx, a.ID, 1_400_000, true); err != nil {
		t.Fatal(err)
	}
	if i, _, _ := imports.ByID(ctx, a.ID); len(i.Review) != 0 || i.AdPrice != 1_400_000 || !i.AdGone {
		t.Fatalf("followed: %+v", i)
	}
	if err = imports.Sold(ctx, a.ID, uuid.NewV7(), now); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM events WHERE kind = 'chotot_import.sold' AND subject_id = @id`,
		pgx.StrictNamedArgs{"id": a.ID}); n != 1 {
		t.Fatalf("%d events", n)
	}
	if err = imports.DeleteListing(ctx, listing); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := imports.ByID(ctx, a.ID); found {
		t.Fatal("delete listing")
	}
	if _, err = link(t, db, me, profile); err != nil {
		t.Fatal(err)
	}
	if err = repository.NewLinks(db).DeleteAccount(ctx, me); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chotot_codes", "chotot_links", "chotot_imports"} {
		if n := count(t, db, `SELECT count(*) FROM `+table+` WHERE account_id = @id`, pgx.StrictNamedArgs{"id": me}); n != 0 {
			t.Fatalf("%s kept %d rows", table, n)
		}
	}
}
