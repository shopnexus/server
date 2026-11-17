package repository_test

import (
	"bytes"
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
)

func TestVectorState(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	vs := repository.NewVectors(db)
	id := catalogtest.Listing(t, db, accounttest.New(t, db))
	now := time.Now()

	st, err := vs.State(ctx, id, "t1", "p1")
	if err != nil || st.TextHash != nil || len(st.Photos) != 0 {
		t.Fatalf("fresh listing: %+v %v", st, err)
	}
	hash := domain.TextHash("t1", "x")
	if err = vs.PutText(ctx, id, "t1", hash, embedtest.Axis(domain.TextDims, 0), now); err != nil {
		t.Fatal(err)
	}
	if err = vs.PutText(ctx, id, "t1", hash, embedtest.Axis(domain.TextDims, 1), now); err != nil {
		t.Fatalf("a second put replaces: %v", err)
	}
	for _, k := range []string{"media/a.jpg", "media/b.jpg"} {
		if err = vs.PutPhoto(ctx, id, k, "p1", embedtest.Axis(domain.PhotoDims, 0), now); err != nil {
			t.Fatal(err)
		}
	}
	if err = vs.PutPhoto(ctx, id, "media/c.jpg", "p0", embedtest.Axis(domain.PhotoDims, 0), now); err != nil {
		t.Fatal(err)
	}
	st, _ = vs.State(ctx, id, "t1", "p1")
	slices.Sort(st.Photos)
	if !bytes.Equal(st.TextHash, hash) || !slices.Equal(st.Photos, []string{"media/a.jpg", "media/b.jpg"}) {
		t.Fatalf("%+v: another version's photo counts as missing", st)
	}
	if st, _ = vs.State(ctx, id, "t2", "p1"); st.TextHash != nil {
		t.Fatal("another text version counts as missing")
	}
	if err = vs.DropPhotosExcept(ctx, id, []string{"media/b.jpg"}); err != nil {
		t.Fatal(err)
	}
	if st, _ = vs.State(ctx, id, "t1", "p1"); !slices.Equal(st.Photos, []string{"media/b.jpg"}) {
		t.Fatal(st.Photos)
	}
}

func TestVectorSource(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	vs := repository.NewVectors(db)
	id := catalogtest.Listing(t, db, accounttest.New(t, db))
	now := time.Now()
	if text, photo, err := vs.Source(ctx, id, "t1", "p1"); err != nil || text != nil || photo != nil {
		t.Fatalf("no vectors: %v %v %v", text, photo, err)
	}
	_ = vs.PutText(ctx, id, "t1", []byte{1}, embedtest.Axis(domain.TextDims, 2), now)
	_ = vs.PutPhoto(ctx, id, "media/a.jpg", "p1", embedtest.Axis(domain.PhotoDims, 0), now)
	_ = vs.PutPhoto(ctx, id, "media/b.jpg", "p1", embedtest.Axis(domain.PhotoDims, 1), now)
	text, photo, err := vs.Source(ctx, id, "t1", "p1")
	if err != nil || len(text) != domain.TextDims || text[2] != 1 || len(photo) != domain.PhotoDims {
		t.Fatalf("%v %v", len(text), err)
	}
	if math.Abs(float64(photo[0])-math.Sqrt2/2) > 0.01 || math.Abs(float64(photo[1])-math.Sqrt2/2) > 0.01 {
		t.Fatalf("the photo source is the normalized mean: %v %v", photo[0], photo[1])
	}
}

func TestEmbeddable(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	seller := accounttest.New(t, db)
	on := catalogtest.Listing(t, db, seller)
	gone := catalogtest.Listing(t, db, seller)
	ls := repository.NewListings(db)
	l, _ := ls.Get(ctx, gone)
	if err := l.Delete(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ls.Save(ctx, &l, l.WriteID(), nil); err != nil { // the same write ID as read is a normal write
		t.Fatal(err)
	}
	ids, err := repository.NewVectors(db).Embeddable(ctx)
	if err != nil || !slices.Contains(ids, on) || slices.Contains(ids, gone) {
		t.Fatalf("%v %v", ids, err)
	}
}
