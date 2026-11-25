package repository_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
)

type item struct {
	title  string
	desc   string
	cat    string
	price  int64
	point  domain.Point
	age    time.Duration
	status domain.Status
	// commune is 00004, in Hà Nội, when empty.
	commune string
}

// put stores an item as a published listing of a fresh seller.
func put(t *testing.T, db *pgxpool.Pool, it item) uuid.UUID {
	t.Helper()
	status := it.status
	if status == "" {
		status = domain.StatusActive
	}
	commune := it.commune
	if commune == "" {
		commune = "00004"
	}
	at := time.Now().Add(-it.age)
	id := uuid.NewV7()
	l := domain.RestoreListing(domain.ListingState{
		ID: id, SellerID: accounttest.New(t, db), Title: it.title, Description: it.desc, Price: it.price, CategoryCode: it.cat,
		Condition: domain.ConditionUsed, AllowOffers: true, CommuneCode: commune,
		Photos: []domain.Photo{{UploadID: uuid.NewV7()}}, Status: status, WriteID: id,
		CreatedAt: at, UpdatedAt: at, PublishedAt: &at, Lat: it.point.Lat, Lng: it.point.Lng,
	})
	if _, err := repository.NewListings(db).Insert(context.Background(), l, uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSearch(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewSearches(db)
	hn := domain.Point{Lat: 21.03, Lng: 105.85}
	touring := put(t, db, item{title: "Xe đạp touring Giant", cat: "vehicles.bicycle", price: 2_500_000, point: hn, age: 24 * time.Hour})
	mtb := put(t, db, item{
		title: "Xe đạp địa hình", cat: "vehicles.bicycle", price: 1_800_000,
		point: domain.Point{Lat: 21.20, Lng: 105.85}, age: 20 * 24 * time.Hour,
	})
	lamp := put(t, db, item{title: "Đèn xe đạp", cat: "sports.other", price: 150_000, point: hn, age: time.Hour})
	put(t, db, item{title: "Xe đạp trẻ em", cat: "baby.toys", price: 900_000, point: hn, status: domain.StatusReserved})
	put(t, db, item{title: "iPhone 13", cat: "phones.iphone", price: 8_000_000, point: hn})

	ids := func(q domain.Search) []uuid.UUID {
		t.Helper()
		q.Limit = 20
		hits, err := s.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]uuid.UUID, len(hits))
		for i, h := range hits {
			out[i] = h.ID
		}
		return out
	}
	text := []domain.Phrase{{Text: "xe đạp", Weight: 1}}

	if got := ids(domain.Search{Phrases: text, Sort: domain.SortRelevance}); len(got) != 3 {
		t.Fatalf("only matching, active listings: %v", got)
	}
	got := ids(domain.Search{Phrases: text, Sort: domain.SortRelevance, Boosts: []domain.Boost{
		{Kind: domain.BoostCategory, Code: "vehicles.bicycle", Weight: 1.5},
		{Kind: domain.BoostCategoryRoot, Code: "sports", Weight: -1.5},
	}})
	if len(got) != 3 || got[2] != lamp {
		t.Fatalf("a demote puts the accessory last: %v", got)
	}
	got = ids(domain.Search{
		Phrases: text, Sort: domain.SortRelevance, Filters: domain.Filters{CategoryCode: "vehicles.bicycle"},
		Boosts: []domain.Boost{{Kind: domain.BoostNear, Near: hn, RadiusKm: 5, Weight: 2}},
	})
	if len(got) != 2 || got[0] != touring {
		t.Fatalf("a near ring puts the Hà Nội bike first: %v", got)
	}
	if got = ids(domain.Search{
		Phrases: text, Sort: domain.SortRelevance, Filters: domain.Filters{PriceMax: 2_000_000, CategoryRoot: "vehicles"},
	}); len(got) != 1 || got[0] != mtb {
		t.Fatalf("a narrow filter keeps its one match: %v", got)
	}
	if got = ids(domain.Search{Sort: domain.SortPriceAsc, Filters: domain.Filters{Origin: &hn, RadiusKm: 10}}); len(got) != 3 || got[0] != lamp {
		t.Fatalf("a browse by price inside 10 km: %v", got)
	}
	if got = ids(domain.Search{Sort: domain.SortNearest, Filters: domain.Filters{Origin: &hn, RadiusKm: 50, CategoryCode: "vehicles.bicycle"}}); len(got) != 2 || got[1] != mtb {
		t.Fatalf("nearest first: %v", got)
	}
	if got = ids(domain.Search{
		Sort: domain.SortRelevance, Filters: domain.Filters{CategoryRoot: "vehicles"},
		Boosts: []domain.Boost{{Kind: domain.BoostFresh, Since: time.Now().Add(-3 * 24 * time.Hour), Weight: 1}},
	}); len(got) != 2 || got[0] != touring {
		t.Fatalf("a fresh ring orders a browse: %v", got)
	}
	counts, err := s.Counts(ctx, domain.Search{Phrases: text, Filters: domain.Filters{CategoryRoot: "vehicles"}})
	if err != nil || len(counts) != 1 || counts[0].Code != "vehicles.bicycle" || counts[0].Count != 2 {
		t.Fatalf("counts under a root: %v %v", counts, err)
	}
	boosted, err := s.Counts(ctx, domain.Search{
		Phrases: text, Filters: domain.Filters{CategoryRoot: "vehicles"},
		Boosts: []domain.Boost{{Kind: domain.BoostCategory, Code: "vehicles.bicycle", Weight: 1.5}},
	})
	if err != nil || len(boosted) != 1 || boosted[0].Count != 2 {
		t.Fatalf("counts leave the boosts out: %v %v", boosted, err)
	}
	if got = ids(domain.Search{Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1}, {Text: "đèn", Weight: -2}}, Sort: domain.SortRelevance}); len(got) != 3 || got[2] != lamp {
		t.Fatalf("a demoted phrase puts the lamp last: %v", got)
	}
	all, err := s.Counts(ctx, domain.Search{})
	if err != nil || !slices.Contains(all, domain.CategoryCount{Code: "vehicles.bicycle", Count: 2}) {
		t.Fatalf("counts of every listing: %v %v", all, err)
	}
	if got = ids(domain.Search{Phrases: []domain.Phrase{{Text: "?!", Weight: 1}}, Sort: domain.SortRelevance}); len(got) != 0 {
		t.Fatalf("punctuation matches nothing: %v", got)
	}
	if got = ids(domain.Search{Phrases: []domain.Phrase{{Text: "giabt", Weight: 1}}, Sort: domain.SortRelevance}); len(got) != 1 || got[0] != touring {
		t.Fatalf("a typo in a long word matches through fuzzy: %v", got)
	}
}

// AllWords needs every word of a phrase in the title, description or specs;
// the IDs filter and the exclusions hold in BM25 and in browsing.
// A folded phrase matches the text with diacritics folded away; the same
// words as written miss a title that has them.
func TestSearchFolded(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewSearches(db)
	bike := put(t, db, item{title: "Xe đạp đi làm Asama", cat: "vehicles.bicycle", price: 2_000_000})
	put(t, db, item{title: "Ổ cứng SSD 512GB", cat: "laptop.parts", price: 900_000, desc: "Đã dùng 2 tháng"})
	search := func(p domain.Phrase) []uuid.UUID {
		t.Helper()
		hits, err := s.Search(ctx, domain.Search{Phrases: []domain.Phrase{p}, Sort: domain.SortRelevance, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		out := make([]uuid.UUID, len(hits))
		for i, h := range hits {
			out[i] = h.ID
		}
		return out
	}
	if got := search(domain.Phrase{Text: "dap asama", Weight: 1, Folded: true}); len(got) != 1 || got[0] != bike {
		t.Fatalf("folded: %v", got)
	}
	if got := search(domain.Phrase{Text: "dap", Weight: 1}); len(got) != 0 {
		t.Fatalf("as written, dap missed đạp: %v", got)
	}
	if got := search(domain.Phrase{Text: "xe dap di lam", Weight: 1, Folded: true, AllWords: true}); len(got) != 1 || got[0] != bike {
		t.Fatalf("every folded word: %v", got)
	}
}

func TestSearchAllWordsAndExclusions(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewSearches(db)
	hn := domain.Point{Lat: 21.03, Lng: 105.85}
	bike := put(t, db, item{title: "Xe đạp Asama", cat: "vehicles.bicycle", price: 1_500_000, point: hn})
	motor := put(t, db, item{title: "Xe máy Wave", cat: "vehicles.bicycle", price: 9_000_000, point: hn})
	split := put(t, db, item{title: "Đạp êm, còn mới", desc: "xe của con gái", cat: "vehicles.bicycle", price: 900_000, point: hn})
	byID := func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }
	run := func(q domain.Search) []uuid.UUID {
		t.Helper()
		q.Limit = 20
		hits, err := s.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]uuid.UUID, len(hits))
		for i, h := range hits {
			out[i] = h.ID
		}
		slices.SortFunc(out, byID)
		return out
	}
	want := func(ids ...uuid.UUID) []uuid.UUID {
		slices.SortFunc(ids, byID)
		return ids
	}
	all := []uuid.UUID{bike, motor, split}
	words := domain.Search{Filters: domain.Filters{IDs: all}, Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1, AllWords: true}}}
	if got := run(words); !slices.Equal(got, want(bike, split)) {
		t.Fatalf("all words: %v", got)
	}
	ranked := domain.Search{Filters: domain.Filters{IDs: all}, Phrases: []domain.Phrase{{Text: "xe đạp", Weight: 1}}}
	if got := run(ranked); !slices.Contains(got, motor) {
		t.Fatalf("a ranked phrase matches any word: %v", got)
	}
	empty := domain.Search{Filters: domain.Filters{IDs: all}, Phrases: []domain.Phrase{{Text: "- / ...", Weight: 1, AllWords: true}}}
	if got := run(empty); len(got) != 0 {
		t.Fatalf("a phrase of no word matches nothing: %v", got)
	}
	if got := run(domain.Search{Filters: domain.Filters{IDs: []uuid.UUID{bike}}, Sort: domain.SortNewest}); !slices.Equal(got, []uuid.UUID{bike}) {
		t.Fatalf("ids: %v", got)
	}
	ex := domain.Search{Filters: domain.Filters{IDs: all}, Sort: domain.SortNewest, ExcludeListings: []uuid.UUID{bike}}
	if got := run(ex); !slices.Equal(got, want(motor, split)) {
		t.Fatalf("exclude listings: %v", got)
	}
	var seller uuid.UUID
	if err := db.QueryRow(ctx, `SELECT seller_id FROM listings WHERE id = $1`, motor).Scan(&seller); err != nil {
		t.Fatal(err)
	}
	ex = domain.Search{Filters: domain.Filters{IDs: all}, Phrases: []domain.Phrase{{Text: "xe", Weight: 1}}, ExcludeSellers: []uuid.UUID{seller}}
	if got := run(ex); slices.Contains(got, motor) || len(got) != 2 {
		t.Fatalf("exclude sellers: %v", got)
	}
}

// SortPointsNewest puts the listings of the heavier boosts first and every
// other active listing after them, each group newest first; without boosts
// it is the newest order. Filters hold, and any page matches the full
// order.
func TestSearchPointsNewest(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewSearches(db)
	hn := domain.Point{Lat: 21.03, Lng: 105.85}
	toyOld := put(t, db, item{title: "Ngựa gỗ", cat: "baby.toys", price: 300_000, point: hn, age: 3 * time.Hour})
	toyNew := put(t, db, item{title: "Xếp hình", cat: "baby.toys", price: 200_000, point: hn, age: time.Hour})
	gym := put(t, db, item{title: "Tạ tay", cat: "sports.gym", price: 400_000, point: hn, age: 30 * time.Minute})
	phoneOld := put(t, db, item{title: "iPhone 12", cat: "phones.iphone", price: 7_000_000, point: hn, age: 2 * time.Hour})
	phoneNew := put(t, db, item{title: "iPhone 13", cat: "phones.iphone", price: 9_000_000, point: hn, age: 10 * time.Minute})
	hidden := put(t, db, item{title: "Búp bê", cat: "baby.toys", price: 100_000, point: hn, age: 5 * time.Minute})
	put(t, db, item{title: "Xe đẩy đồ chơi", cat: "baby.toys", price: 500_000, point: hn, status: domain.StatusReserved})
	run := func(q domain.Search) []uuid.UUID {
		t.Helper()
		q.Limit = 20
		hits, err := s.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]uuid.UUID, len(hits))
		for i, h := range hits {
			out[i] = h.ID
		}
		return out
	}
	boosts := []domain.Boost{
		{Kind: domain.BoostCategory, Code: "baby.toys", Weight: 2},
		{Kind: domain.BoostCategory, Code: "sports.gym", Weight: 1},
	}
	got := run(domain.Search{Sort: domain.SortPointsNewest, Boosts: boosts, ExcludeListings: []uuid.UUID{hidden}})
	if want := []uuid.UUID{toyNew, toyOld, gym, phoneNew, phoneOld}; !slices.Equal(got, want) {
		t.Fatalf("points then newest: %v, want %v", got, want)
	}
	newest := run(domain.Search{Sort: domain.SortNewest})
	if got = run(domain.Search{Sort: domain.SortPointsNewest}); !slices.Equal(got, newest) || len(got) != 6 {
		t.Fatalf("no boosts: %v, newest %v", got, newest)
	}
	// Each branch reads offset+limit listings, so every page of the order
	// is the page of the full sort.
	full := []uuid.UUID{toyNew, toyOld, gym, phoneNew, phoneOld}
	for offset := range full {
		for limit := 1; offset+limit <= len(full); limit++ {
			hits, err := s.Search(ctx, domain.Search{
				Sort: domain.SortPointsNewest, Boosts: boosts, ExcludeListings: []uuid.UUID{hidden}, Offset: offset, Limit: limit,
			})
			if err != nil {
				t.Fatal(err)
			}
			page := make([]uuid.UUID, len(hits))
			for i, h := range hits {
				page[i] = h.ID
			}
			if want := full[offset : offset+limit]; !slices.Equal(page, want) {
				t.Fatalf("offset %d limit %d: %v, want %v", offset, limit, page, want)
			}
		}
	}
	if got = run(domain.Search{Sort: domain.SortPointsNewest, Boosts: boosts, Filters: domain.Filters{CategoryRoot: "phones"}}); !slices.Equal(got, []uuid.UUID{phoneNew, phoneOld}) {
		t.Fatalf("a root filter: %v", got)
	}
	if got = run(domain.Search{Sort: domain.SortPointsNewest, Boosts: boosts, Filters: domain.Filters{CategoryCode: "sports.gym"}}); !slices.Equal(got, []uuid.UUID{gym}) {
		t.Fatalf("a subcategory filter: %v", got)
	}
	if _, err := s.Search(ctx, domain.Search{
		Sort: domain.SortPointsNewest, Limit: 20, Boosts: []domain.Boost{{Kind: domain.BoostFresh, Since: time.Now(), Weight: 1}},
	}); err == nil {
		t.Fatal("a fresh boost was taken")
	}
}

// A province keeps the listings of its communes in every kind of search
// and in counts.
func TestSearchProvince(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewSearches(db)
	hn, hcm := domain.Point{Lat: 21.03, Lng: 105.85}, domain.Point{Lat: 10.97, Lng: 106.66}
	bikeHN := put(t, db, item{title: "Xe đạp Asama", cat: "vehicles.bicycle", price: 1_500_000, point: hn, age: time.Hour})
	phoneHN := put(t, db, item{title: "iPhone 12", cat: "phones.iphone", price: 7_000_000, point: hn, age: 2 * time.Hour})
	bikeHCM := put(t, db, item{title: "Xe đạp Giant", cat: "vehicles.bicycle", price: 2_500_000, point: hcm, commune: "25747"})
	run := func(q domain.Search) []uuid.UUID {
		t.Helper()
		q.Limit = 20
		hits, err := s.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]uuid.UUID, len(hits))
		for i, h := range hits {
			out[i] = h.ID
		}
		return out
	}
	hanoi := domain.Filters{Province: "01"}
	if got := run(domain.Search{Filters: hanoi, Sort: domain.SortNewest}); !slices.Equal(got, []uuid.UUID{bikeHN, phoneHN}) {
		t.Fatalf("browse: %v", got)
	}
	text := []domain.Phrase{{Text: "xe đạp", Weight: 1}}
	if got := run(domain.Search{Filters: domain.Filters{Province: "79"}, Phrases: text, Sort: domain.SortRelevance}); !slices.Equal(got, []uuid.UUID{bikeHCM}) {
		t.Fatalf("phrases: %v", got)
	}
	if got := run(domain.Search{Filters: domain.Filters{Province: "01", CategoryRoot: "vehicles"}, Phrases: text}); !slices.Equal(got, []uuid.UUID{bikeHN}) {
		t.Fatalf("with a root: %v", got)
	}
	boosts := []domain.Boost{{Kind: domain.BoostCategory, Code: "phones.iphone", Weight: 1}}
	if got := run(domain.Search{Filters: hanoi, Sort: domain.SortPointsNewest, Boosts: boosts}); !slices.Equal(got, []uuid.UUID{phoneHN, bikeHN}) {
		t.Fatalf("points: %v", got)
	}
	if got := run(domain.Search{Filters: domain.Filters{Province: "31"}, Sort: domain.SortNewest}); len(got) != 0 {
		t.Fatalf("a province with none: %v", got)
	}
	counts, err := s.Counts(ctx, domain.Search{Filters: domain.Filters{Province: "79"}})
	if err != nil || !slices.Equal(counts, []domain.CategoryCount{{Code: "vehicles.bicycle", Count: 1}}) {
		t.Fatalf("counts: %v %v", counts, err)
	}
	const td = domain.TextDims
	vs := repository.NewVectors(db)
	for _, id := range []uuid.UUID{bikeHN, bikeHCM} {
		if err = vs.PutText(ctx, id, "t", []byte{1}, embedtest.Axis(td, 0), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	hybrid := domain.Search{
		Filters: domain.Filters{Province: "79"}, Phrases: text, Vectors: vecs(embedtest.Axis(td, 0), nil),
		Weights: domain.Weights{BM25: 1, Text: 1}, Sort: domain.SortRelevance,
	}
	if got := run(hybrid); !slices.Equal(got, []uuid.UUID{bikeHCM}) {
		t.Fatalf("hybrid: %v", got)
	}
	if counts, err = s.Counts(ctx, hybrid); err != nil || !slices.Equal(counts, []domain.CategoryCount{{Code: "vehicles.bicycle", Count: 1}}) {
		t.Fatalf("hybrid counts: %v %v", counts, err)
	}
}
