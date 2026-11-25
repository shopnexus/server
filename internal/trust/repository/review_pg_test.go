package repository_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/chat/chattest"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealrepo "github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// order stores a requested order of buyer's on a new listing of seller's
// and returns the order and the listing.
func order(t *testing.T, db *pgxpool.Pool, buyer, seller uuid.UUID) (orderID, listingID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	listing := catalogtest.Listing(t, db, seller)
	deals := dealrepo.NewDeals(db)
	d := dealdomain.Open(uuid.NewV7(), chattest.Pair(t, db, buyer, seller), listing, buyer, seller, now)
	if _, err := deals.Insert(ctx, d); err != nil {
		t.Fatal(err)
	}
	onSale := dealdomain.Listing{Price: 1_500_000, Status: dealdomain.ListingActive}
	r := dealdomain.Recipient{Name: "Hân", Phone: "+84912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: dealdomain.AddressHome}
	if err := d.RequestCod(dealdomain.Buyer, uuid.NewV7(), 1_500_000, r, onSale, now); err != nil {
		t.Fatal(err)
	}
	if err := deals.Save(ctx, &d, uuid.NewV7(), uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	return d.Order().ID, listing
}

// review stores buyer's review of seller, written at postedAt and visible
// from now; an edit before now finds it hidden, so it may change.
func review(t *testing.T, s repository.Reviews, o, seller, buyer, listing uuid.UUID, stars int) domain.Review {
	t.Helper()
	r, err := domain.NewReview(uuid.NewV7(), o, domain.RoleSeller, buyer, seller, listing, stars, "Ổn", nil, nil, now, postedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	return r
}

// postedAt is when review writes a review, before it shows at now.
var postedAt = now.Add(-2 * time.Hour)

func TestReviews(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewReviews(db)
	ctx := context.Background()
	seller, one, two := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	o1, l1 := order(t, db, one, seller)
	o2, l2 := order(t, db, two, seller)
	r1 := review(t, s, o1, seller, one, l1, 4)
	r2 := review(t, s, o2, seller, two, l2, 2)
	var posted int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'review.posted' AND id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": []uuid.UUID{r1.ID(), r2.ID()}}).Scan(&posted); err != nil || posted != 2 {
		t.Fatalf("one review.posted per review: %d %v", posted, err)
	}
	if avg, n, err := s.Summary(ctx, seller, domain.RoleSeller, now); err != nil || n != 2 || avg != 3 {
		t.Fatalf("summary %v %d %v", avg, n, err)
	}
	if err := r1.Edit(5, "Rất tốt", nil, nil, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &r1, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	got, err := s.ByOrder(ctx, o1, domain.RoleSeller)
	if err != nil || got.Stars() != 5 || got.Text() != "Rất tốt" || !got.CreatedAt().Equal(postedAt) {
		t.Fatalf("%+v %v", got.State(), err)
	}
	if avg, n, _ := s.Summary(ctx, seller, domain.RoleSeller, now); n != 2 || avg != 3.5 {
		t.Fatalf("after the edit %v %d", avg, n)
	}
	list, err := s.OfSubject(ctx, seller, domain.RoleSeller, now, page.Request{})
	if err != nil || len(list) != 2 || list[0].OrderID() != o2 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	if err = r2.Delete(now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &r2, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if avg, n, _ := s.Summary(ctx, seller, domain.RoleSeller, now); n != 1 || avg != 5 {
		t.Fatalf("after the delete %v %d", avg, n)
	}
	if n, countErr := s.CountByRole(ctx); countErr != nil || !maps.Equal(n, map[domain.Role]int{domain.RoleSeller: 1}) {
		t.Fatalf("count without the deleted one: %v %v", n, countErr)
	}
	if got, err = s.ByOrder(ctx, o2, domain.RoleSeller); err != nil || !got.Deleted() || got.Text() != "" {
		t.Fatalf("deleted: %+v %v", got.State(), err)
	}
	// The batch read returns the orders' reviews, deleted ones too, and
	// skips an order without one.
	o3, _ := order(t, db, one, seller)
	all, err := s.OfOrders(ctx, []uuid.UUID{o1, o2, o3})
	if err != nil || len(all) != 2 {
		t.Fatalf("the orders' reviews: %d %v", len(all), err)
	}
	byOrder := map[uuid.UUID]domain.Review{all[0].OrderID(): all[0], all[1].OrderID(): all[1]}
	if a, b := byOrder[o1], byOrder[o2]; a.ID() != r1.ID() || a.Stars() != 5 || b.ID() != r2.ID() || !b.Deleted() {
		t.Fatalf("the orders' reviews: %+v %+v", a.State(), b.State())
	}
	if none, noneErr := s.OfOrders(ctx, nil); noneErr != nil || len(none) != 0 {
		t.Fatalf("no orders: %d %v", len(none), noneErr)
	}
	if avg, n, err := s.Summary(ctx, accounttest.New(t, db), domain.RoleSeller, now); err != nil || n != 0 || avg != 0 {
		t.Fatalf("no reviews: %v %d %v", avg, n, err)
	}
}

// Tags are stored and edited with the review, and counted over the visible
// reviews that are not deleted, most used first.
func TestReviewTags(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewReviews(db)
	ctx := context.Background()
	seller := accounttest.New(t, db)
	reviews := make([]domain.Review, 0, 4)
	for _, tags := range [][]domain.Tag{
		{domain.TagWellPacked, domain.TagQuickReply}, {domain.TagQuickReply}, {domain.TagAsDescribed}, nil,
	} {
		// A buyer has at most 3 open COD orders, so each order has its own.
		buyer := accounttest.New(t, db)
		o, l := order(t, db, buyer, seller)
		r, err := domain.NewReview(uuid.NewV7(), o, domain.RoleSeller, buyer, seller, l, 5, "", tags, nil, now, postedAt)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Insert(ctx, r); err != nil {
			t.Fatal(err)
		}
		reviews = append(reviews, r)
	}
	got, err := s.Get(ctx, reviews[0].ID())
	if err != nil || !slices.Equal(got.Tags(), []domain.Tag{domain.TagWellPacked, domain.TagQuickReply}) {
		t.Fatalf("%+v %v", got.State(), err)
	}
	if got, err = s.Get(ctx, reviews[3].ID()); err != nil || got.Tags() != nil {
		t.Fatalf("no tags: %+v %v", got.State(), err)
	}
	if counts, _ := s.TagCounts(ctx, seller, domain.RoleSeller, postedAt); len(counts) != 0 {
		t.Fatalf("hidden: %+v", counts)
	}
	if err = reviews[3].Edit(5, "", []domain.Tag{domain.TagQuickReply}, nil, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = reviews[2].Delete(now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := range reviews[2:] {
		if err = s.Save(ctx, &reviews[2+i], uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
	}
	want := []repository.TagCount{{Tag: domain.TagQuickReply, Count: 3}, {Tag: domain.TagWellPacked, Count: 1}}
	if counts, err := s.TagCounts(ctx, seller, domain.RoleSeller, now); err != nil || !slices.Equal(counts, want) {
		t.Fatalf("%+v %v", counts, err)
	}
	if counts, err := s.TagCounts(ctx, seller, domain.RoleBuyer, now); err != nil || len(counts) != 0 {
		t.Fatalf("other role: %+v %v", counts, err)
	}
}

func TestReviewWritesConflict(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewReviews(db)
	ctx := context.Background()
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	o, l := order(t, db, buyer, seller)
	first := review(t, s, o, seller, buyer, l, 5)
	second, err := domain.NewReview(uuid.NewV7(), o, domain.RoleSeller, buyer, seller, l, 1, "Lần hai", nil, nil, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(ctx, second); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a second first review: %v", err)
	}
	stale, err := s.ByOrder(ctx, o, domain.RoleSeller)
	if err != nil || stale.ID() != first.ID() {
		t.Fatalf("%+v %v", stale.State(), err)
	}
	fresh := stale
	if err = fresh.Delete(now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	replay, w := fresh, uuid.NewV7()
	if err = s.Save(ctx, &fresh, w); err != nil {
		t.Fatal(err)
	}
	// A retry of the same save finds its own write ID.
	if err = s.Save(ctx, &replay, w); err != nil {
		t.Fatalf("a retried save: %v", err)
	}
	if err = stale.Edit(2, "Cũ", nil, nil, now.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a stale edit: %v", err)
	}
	if _, n, _ := s.Summary(ctx, seller, domain.RoleSeller, now); n != 0 {
		t.Fatalf("a deleted review counted: %d", n)
	}
	if list, _ := s.OfSubject(ctx, seller, domain.RoleSeller, now, page.Request{}); len(list) != 0 {
		t.Fatalf("a deleted review listed: %d", len(list))
	}
}

// The second side's review shows the first from its own creation; a first
// reply writes review.replied once.
func TestRevealAndReply(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewReviews(db)
	ctx := context.Background()
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	o, l := order(t, db, buyer, seller)
	deadline := now.Add(domain.BlindWindow)
	first, err := domain.NewReview(uuid.NewV7(), o, domain.RoleSeller, buyer, seller, l, 5, "Tốt", nil, nil, deadline, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, n, _ := s.Summary(ctx, seller, domain.RoleSeller, now.Add(time.Hour)); n != 0 {
		t.Fatalf("a hidden review counted: %d", n)
	}
	if _, n, _ := s.Summary(ctx, seller, domain.RoleSeller, deadline); n != 1 {
		t.Fatalf("at the deadline: %d", n)
	}
	// A hidden second review while the first exists is refused.
	later := now.Add(2 * time.Hour)
	stale, err := domain.NewReview(uuid.NewV7(), o, domain.RoleBuyer, seller, buyer, l, 4, "", nil, nil, deadline, later)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(ctx, stale); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a hidden second review: %v", err)
	}
	// An edit that read the first review hidden must not land once it shows.
	hidden, err := s.ByOrder(ctx, o, domain.RoleSeller)
	if err != nil {
		t.Fatal(err)
	}
	second, err := domain.NewReview(uuid.NewV7(), o, domain.RoleBuyer, seller, buyer, l, 4, "", nil, nil, later, later)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err := s.ByOrder(ctx, o, domain.RoleSeller)
	if err != nil || !got.VisibleAt().Equal(later) || got.WriteID() != second.WriteID() {
		t.Fatalf("revealed: %+v %v", got.State(), err)
	}
	if err = hidden.Edit(1, "Đổi ý", nil, nil, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &hidden, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("an edit read before the reveal: %v", err)
	}
	if list, _ := s.OfSubject(ctx, buyer, domain.RoleBuyer, later, page.Request{}); len(list) != 1 || list[0].ID() != second.ID() {
		t.Fatalf("buyer's reviews: %d", len(list))
	}
	if err = got.Reply(seller, "Cảm ơn", later); err != nil {
		t.Fatal(err)
	}
	if err = s.Save(ctx, &got, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Get(ctx, first.ID()); err != nil || got.ReplyText() != "Cảm ơn" || got.RepliedAt() == nil {
		t.Fatalf("reply: %+v %v", got.State(), err)
	}
	var replied int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'review.replied' AND subject_id = @id`,
		pgx.StrictNamedArgs{"id": first.ID()}).Scan(&replied); err != nil || replied != 1 {
		t.Fatalf("review.replied: %d %v", replied, err)
	}
}

// A review keeps its photos in order; helpful marks count once per person,
// and the viewer's own mark shows in one query over a page.
func TestReviewPhotosAndHelpfulMarks(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewReviews(db)
	ctx := context.Background()
	seller, buyer, reader := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	o, l := order(t, db, buyer, seller)
	photos := []domain.Photo{{UploadID: uuid.NewV7(), PhotoKey: "media/b.jpg", ThumbKey: "media/b_thumb.jpg"}, {UploadID: uuid.NewV7(), PhotoKey: "media/a.jpg", ThumbKey: "media/a_thumb.jpg"}}
	r, err := domain.NewReview(uuid.NewV7(), o, domain.RoleSeller, buyer, seller, l, 5, "Như hình", nil, photos, now, postedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Insert(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, r.ID())
	if err != nil || len(got.Photos()) != 2 || got.Photos()[0] != photos[0] || got.Photos()[1] != photos[1] {
		t.Fatalf("%+v %v", got.Photos(), err)
	}
	for _, who := range []uuid.UUID{reader, reader, buyer} {
		if err = s.MarkHelpful(ctx, r.ID(), who, now); err != nil {
			t.Fatal(err)
		}
	}
	other := uuid.NewV7()
	marks, err := s.Helpful(ctx, []uuid.UUID{r.ID(), other}, &reader)
	if err != nil || len(marks) != 1 || marks[r.ID()] != (repository.HelpfulMarks{Count: 2, Viewer: true}) {
		t.Fatalf("%+v %v", marks, err)
	}
	if marks, err = s.Helpful(ctx, []uuid.UUID{r.ID()}, nil); err != nil || marks[r.ID()] != (repository.HelpfulMarks{Count: 2}) {
		t.Fatalf("signed out: %+v %v", marks, err)
	}
	if err = s.UnmarkHelpful(ctx, r.ID(), reader); err != nil {
		t.Fatal(err)
	}
	if err = s.UnmarkHelpful(ctx, r.ID(), reader); err != nil {
		t.Fatal(err)
	}
	if marks, err = s.Helpful(ctx, []uuid.UUID{r.ID()}, &reader); err != nil || marks[r.ID()] != (repository.HelpfulMarks{Count: 1}) {
		t.Fatalf("unmarked: %+v %v", marks, err)
	}
}
