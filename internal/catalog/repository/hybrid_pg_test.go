package repository_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
)

// put lists a listing at price with a text vector and one photo vector.
func putHybrid(t *testing.T, db *pgxpool.Pool, seller uuid.UUID, title string, price int64, text, photo []float32) uuid.UUID {
	t.Helper()
	id := catalogtest.Listing(t, db, seller, func(d *domain.ListingInput, _ *domain.Refs) { d.Title, d.Price = title, price })
	vs := repository.NewVectors(db)
	if text != nil {
		if err := vs.PutText(context.Background(), id, "t", []byte{1}, text, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if photo != nil {
		if err := vs.PutPhoto(context.Background(), id, "media/a.jpg", "p", photo, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func ids(hits []domain.Hit) []uuid.UUID {
	out := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		out[i] = h.ID
	}
	return out
}

func vecs(text, photo []float32) domain.Vectors {
	return domain.Vectors{Text: text, Photo: photo, TextVersion: "t", PhotoVersion: "p", TextMin: 0.5, PhotoMin: 0.5}
}

func TestHybridFusesAndFloors(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := accounttest.New(t, db)
	const td, pd = domain.TextDims, domain.PhotoDims
	exact := putHybrid(t, db, s, "Xe đạp Asama", 1_500_000, embedtest.Axis(td, 0), embedtest.Axis(pd, 0))
	near := putHybrid(t, db, s, "Xe cho bé", 1_200_000, embedtest.Near(td, 0, 1, 0.8), nil)
	far := putHybrid(t, db, s, "Nồi cơm", 500_000, embedtest.Axis(td, 1), embedtest.Axis(pd, 1))
	words := putHybrid(t, db, s, "Xe đạp cũ", 900_000, nil, nil)

	hits, err := repository.NewSearches(db).Search(ctx, domain.Search{
		Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1}},
		Vectors: vecs(embedtest.Axis(td, 0), embedtest.Axis(pd, 0)),
		Weights: domain.Weights{BM25: 1, Text: 1, Photo: 0.5}, Sort: domain.SortRelevance, Limit: 10,
	})
	got := ids(hits)
	if err != nil || len(got) != 3 || got[0] != exact || slices.Contains(got, far) ||
		!slices.Contains(got, near) || !slices.Contains(got, words) {
		t.Fatalf("%v %v (exact %v near %v words %v far %v)", got, err, exact, near, words, far)
	}

	hits, _ = repository.NewSearches(db).Search(ctx, domain.Search{
		Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1}},
		Vectors: vecs(embedtest.Axis(td, 0), nil),
		Weights: domain.Weights{BM25: 1, Text: 1}, Sort: domain.SortPriceAsc, Limit: 10,
	})
	if got = ids(hits); !slices.Equal(got, []uuid.UUID{words, near, exact}) {
		t.Fatalf("price sort orders the candidates only: %v", got)
	}
}

func TestHybridFiltersEveryList(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := accounttest.New(t, db)
	const pd = domain.PhotoDims
	cheap := putHybrid(t, db, s, "Ghế", 200_000, nil, embedtest.Axis(pd, 0))
	pricey := putHybrid(t, db, s, "Ghế gỗ", 5_000_000, nil, embedtest.Near(pd, 0, 1, 0.9))
	hidden := putHybrid(t, db, s, "Ghế nhựa", 300_000, nil, embedtest.Axis(pd, 0))
	ls := repository.NewListings(db)
	l, _ := ls.Get(ctx, hidden)
	if err := l.Hide(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ls.Save(ctx, &l, uuid.NewV7(), nil); err != nil {
		t.Fatal(err)
	}
	hits, err := repository.NewSearches(db).Search(ctx, domain.Search{
		Filters: domain.Filters{PriceMax: 1_000_000},
		Vectors: vecs(nil, embedtest.Axis(pd, 0)), Weights: domain.Weights{Photo: 1}, Sort: domain.SortRelevance, Limit: 10,
	})
	if got := ids(hits); err != nil || !slices.Equal(got, []uuid.UUID{cheap}) {
		t.Fatalf("%v %v (pricey %v hidden %v)", got, err, pricey, hidden)
	}
}

func TestHybridLikeLeavesOutSourceAndSeller(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	const td = domain.TextDims
	source := putHybrid(t, db, other, "Xe đạp Asama", 1_500_000, embedtest.Axis(td, 0), nil)
	mine := putHybrid(t, db, me, "Xe đạp Asama 2", 1_500_000, embedtest.Axis(td, 0), nil)
	theirs := putHybrid(t, db, other, "Xe đạp Asama 3", 1_400_000, embedtest.Axis(td, 0), nil)
	hits, err := repository.NewSearches(db).Search(ctx, domain.Search{
		Phrases: []domain.Phrase{{Text: "Xe đạp Asama", Weight: 1}},
		Vectors: vecs(embedtest.Axis(td, 0), nil), Like: source, ExcludeSellers: []uuid.UUID{me},
		Weights: domain.Weights{BM25: 0.5, Text: 1}, Sort: domain.SortRelevance, Limit: 10,
	})
	if got := ids(hits); err != nil || !slices.Equal(got, []uuid.UUID{theirs}) {
		t.Fatalf("%v %v (source %v mine %v)", got, err, source, mine)
	}
}

func TestHybridPagesWithoutGapsOrRepeats(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := accounttest.New(t, db)
	const pd = domain.PhotoDims
	all := make([]uuid.UUID, 0, 7)
	for i := range 7 {
		all = append(all, putHybrid(t, db, s, "Ghế", 100_000, nil, embedtest.Near(pd, 0, 1, 0.95-float64(i%3)*0.1)))
	}
	for _, sort := range []domain.SortOrder{domain.SortRelevance, domain.SortNewest, domain.SortPriceAsc} {
		var seen []uuid.UUID
		for offset := 0; offset < 9; offset += 3 {
			hits, err := repository.NewSearches(db).Search(ctx, domain.Search{
				Vectors: vecs(nil, embedtest.Axis(pd, 0)), Weights: domain.Weights{Photo: 1}, Sort: sort, Offset: offset, Limit: 3,
			})
			if err != nil {
				t.Fatal(err)
			}
			seen = append(seen, ids(hits)...)
		}
		slices.SortFunc(seen, uuid.UUID.Compare)
		want := slices.Clone(all)
		slices.SortFunc(want, uuid.UUID.Compare)
		if !slices.Equal(seen, want) {
			t.Fatalf("%s: %d seen of %d, or a repeat", sort, len(seen), len(want))
		}
	}
}

func TestHybridBoostList(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := accounttest.New(t, db)
	const td = domain.TextDims
	a := putHybrid(t, db, s, "Xe A", 1_000_000, embedtest.Near(td, 0, 1, 0.9), nil)
	b := putHybrid(t, db, s, "Xe B", 3_000_000, embedtest.Near(td, 0, 1, 0.8), nil)
	search := domain.Search{
		Vectors: vecs(embedtest.Axis(td, 0), nil),
		Boosts:  []domain.Boost{{Kind: domain.BoostPrice, PriceMin: 2_000_000, PriceMax: 4_000_000, Weight: 1.5}},
		Weights: domain.Weights{Text: 1, Boost: 2}, Sort: domain.SortRelevance, Limit: 10,
	}
	hits, err := repository.NewSearches(db).Search(ctx, search)
	if got := ids(hits); err != nil || !slices.Equal(got, []uuid.UUID{b, a}) {
		t.Fatalf("the boost list lifts b: %v %v", got, err)
	}
}

// A relevance page reads a few times its end from each list; other sorts
// read every listing the lists find.
func TestHybridListDepth(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	const td, n = domain.TextDims, 70
	// A seller lists at most 20.
	sellers := make([]uuid.UUID, (n+19)/20)
	for i := range sellers {
		sellers[i] = accounttest.New(t, db)
	}
	all := make([]uuid.UUID, n)
	for i := range n {
		// Less alike and cheaper down the list: the cheapest is the last by relevance.
		all[i] = putHybrid(t, db, sellers[i/20], "Ghế", int64(1_000_000-i*10_000), embedtest.Near(td, 0, 1, 0.95-float64(i)*0.005), nil)
	}
	search := func(sort domain.SortOrder, offset int) domain.Search {
		return domain.Search{
			Vectors: vecs(embedtest.Axis(td, 0), nil), Weights: domain.Weights{Text: 1},
			Sort: sort, Offset: offset, Limit: 5,
		}
	}
	searches := repository.NewSearches(db)

	hits, err := searches.Search(ctx, search(domain.SortRelevance, 0))
	if got := ids(hits); err != nil || !slices.Equal(got, all[:5]) {
		t.Fatalf("first page: %v %v", got, err)
	}
	hits, err = searches.Search(ctx, search(domain.SortRelevance, n-5))
	if got := ids(hits); err != nil || !slices.Equal(got, all[n-5:]) {
		t.Fatalf("a deep page reads deeper lists: %v %v", got, err)
	}
	hits, err = searches.Search(ctx, search(domain.SortPriceAsc, 0))
	if got := ids(hits); err != nil || len(got) == 0 || got[0] != all[n-1] {
		t.Fatalf("price sort orders every candidate: %v %v", got, err)
	}
}
