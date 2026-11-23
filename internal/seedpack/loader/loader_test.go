package loader_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/seedpack"
	"github.com/shopnexus/server/internal/seedpack/loader"
)

func count(t *testing.T, db *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	a := pgx.StrictNamedArgs{}
	if len(args) == 2 {
		a[args[0].(string)] = args[1]
	}
	if err := db.QueryRow(context.Background(), q, a).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The demo pack loads through the server's repositories, a second load
// writes nothing, and wipe takes every row of it.
func TestLoadTwiceThenWipe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	p, err := seedpack.Open(filepath.Join("..", "..", "..", "cmd", "devseed", "data", "demo"))
	if err != nil {
		t.Fatal(err)
	}
	l := loader.New(loader.Deps{Pool: db, Store: objectstoretest.New(t)})
	opts := loader.Options{Vectors: loader.VectorsPack, HistoryRatio: 1, Workers: 4}
	plan, err := l.Plan(ctx, p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.People.Total != 12 || plan.Listings.Total != 44 || plan.Sold.Total != 4 || plan.Follows != 5 {
		t.Fatalf("plan %+v", plan)
	}
	res, err := l.Load(ctx, plan, seedpack.Discard{})
	if err != nil {
		t.Fatal(err)
	}
	if res.People.Written != 12 || res.Listings.Written != 44 || res.Sold.Written != 4 || res.Reviews != 6 || res.Follows != 5 {
		t.Fatalf("first load %+v", res)
	}
	if n := count(t, db, `SELECT count(*) FROM reviews WHERE role = 'buyer'`); n != 2 {
		t.Errorf("%d reviews of buyers", n)
	}
	if n := count(t, db, `SELECT count(*) FROM reviews WHERE reply IS NOT NULL AND replied_at >= visible_at`); n != 2 {
		t.Errorf("%d replies after their review showed", n)
	}
	if n := count(t, db, `SELECT count(*) FROM listings WHERE status = 'active'`); n != 44 {
		t.Errorf("%d active listings", n)
	}
	if n := count(t, db, `SELECT count(*) FROM orders WHERE status = 'completed'`); n != 4 {
		t.Errorf("%d completed orders", n)
	}
	if n := count(t, db, `SELECT count(*) FROM listings WHERE status = 'sold' AND sold_at IS NOT NULL`); n != 4 {
		t.Errorf("%d sold listings", n)
	}

	plan, err = l.Plan(ctx, p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if plan.People.There != 12 || plan.Listings.There != 44 || plan.Sold.There != 4 {
		t.Fatalf("second plan %+v", plan)
	}
	if res, err = l.Load(ctx, plan, seedpack.Discard{}); err != nil {
		t.Fatal(err)
	}
	if res.People.Written+res.Listings.Written+res.Sold.Written+res.Follows+int(res.Objects) != 0 {
		t.Fatalf("second load wrote %+v", res)
	}

	w, err := l.PlanWipe(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if w.People != 12 || w.Listings != 48 {
		t.Fatalf("wipe plan %+v", w)
	}
	if _, err = l.Wipe(ctx, w, seedpack.Discard{}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`SELECT count(*) FROM listings`, `SELECT count(*) FROM uploads`, `SELECT count(*) FROM orders`,
		`SELECT count(*) FROM identities`, `SELECT count(*) FROM reviews`, `SELECT count(*) FROM account_follows`,
	} {
		if n := count(t, db, q); n != 0 {
			t.Errorf("%s = %d after wipe", q, n)
		}
	}
}

func jpegOf(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for i := 0; i < len(m.Pix); i += 4 {
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = c.R, c.G, c.B, 255
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// With --vectors=pack a text vector made from the listing's text and the
// photo vectors are written under the stack's versions; a listing whose
// text changed since gets none.
func TestLoadPackVectors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tree, err := catalogrepo.NewCategories(db).Tree(ctx, locale.Vietnamese)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, c := range tree {
		names[c.Code] = c.DisplayName
	}

	dir := t.TempDir()
	w, err := seedpack.Create(dir, "vectest", "1")
	if err != nil {
		t.Fatal(err)
	}
	photo, thumb, err := seedpack.ProcessPhoto(jpegOf(t, color.RGBA{200, 30, 30, 255}))
	if err != nil {
		t.Fatal(err)
	}
	sha, err := w.AddPhoto(photo, thumb)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	if err = w.Account(seedpack.Account{Key: "a", Name: "An", Phone: "+84900188001", Commune: "26740", JoinedAt: at}); err != nil {
		t.Fatal(err)
	}
	same := seedpack.Listing{
		Key: "same", Seller: "a", Title: "Xe đạp", Price: 900_000, Category: "vehicles.bicycle", Condition: "used",
		Commune: "26740", Specs: []seedpack.Spec{}, Photos: []string{sha}, CreatedAt: at.Add(time.Hour),
	}
	same.TextSHA256 = seedpack.TextSHA256(same, names[same.Category])
	moved := same
	moved.Key, moved.TextSHA256 = "moved", "0000"
	for _, l := range []seedpack.Listing{same, moved} {
		if err = w.Listing(l); err != nil {
			t.Fatal(err)
		}
	}
	w.Manifest.Vectors = seedpack.Vectors{
		Text: &seedpack.VectorSet{Model: "text@1"}, Photo: &seedpack.VectorSet{Model: "photo@1"},
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	text, err := seedpack.CreateMatrix(filepath.Join(dir, seedpack.TextVectors), 2, seedpack.TextDims)
	if err != nil {
		t.Fatal(err)
	}
	ones := make([]float32, seedpack.TextDims)
	for i := range ones {
		ones[i] = 0.03125
	}
	_ = text.SetRow(0, ones)
	_ = text.SetRow(1, ones)
	_ = text.Close()
	ph, err := seedpack.CreateMatrix(filepath.Join(dir, seedpack.PhotoVectors), 1, seedpack.PhotoDims)
	if err != nil {
		t.Fatal(err)
	}
	row := make([]float32, seedpack.PhotoDims)
	row[0] = 1
	_ = ph.SetRow(0, row)
	_ = ph.Close()
	p, err := seedpack.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeKeys(t, dir, []seedpack.PhotoKey{{Listing: "same", Photo: sha}})

	l := loader.New(loader.Deps{Pool: db, Store: objectstoretest.New(t), TextVersion: "text@1", ImageVersion: "photo@1"})
	plan, err := l.Plan(ctx, p, loader.Options{Vectors: loader.VectorsPack, HistoryRatio: 1, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	res, err := l.Load(ctx, plan, seedpack.Discard{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Vectors.Text != 1 || res.Vectors.Photo != 2 {
		t.Fatalf("vectors %+v", res.Vectors)
	}
	sameID := p.Manifest.ListingID(&same)
	want := catalogdomain.TextHash("text@1", same.Input().EmbeddingText(names[same.Category]))
	var hash []byte
	if err = db.QueryRow(ctx, `SELECT text_hash FROM listing_text_vectors WHERE listing_id = @id`, pgx.StrictNamedArgs{"id": sameID}).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, want) {
		t.Error("the text vector is not under the listing's text hash")
	}
	if n := count(t, db, `SELECT count(*) FROM listing_text_vectors WHERE listing_id = @id`, "id", p.Manifest.ListingID(&moved)); n != 0 {
		t.Error("a listing whose text moved got the pack's vector")
	}
}

func writeKeys(t *testing.T, dir string, keys []seedpack.PhotoKey) {
	t.Helper()
	var b bytes.Buffer
	for _, k := range keys {
		b.WriteString(`{"listing":"` + k.Listing + `","photo":"` + k.Photo + `"}` + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, seedpack.PhotoKeysFile), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
