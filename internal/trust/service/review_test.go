package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
)

func TestPutReview(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, order := e.completed(t, buyer, seller)
	v, err := e.trust.PutReview(ctx, buyer, order, 5, "Đúng mô tả", nil, nil)
	if err != nil || v.Stars != 5 || v.Role != domain.RoleSeller || v.Reviewer.ID != buyer || v.Reviewer.DisplayName != "Linh" ||
		v.Listing.Title == "" || v.Reply != nil {
		t.Fatalf("%+v %v", v, err)
	}
	if v, err = e.trust.PutReview(ctx, buyer, order, 4, "Hơi trầy", nil, nil); err != nil || v.Stars != 4 {
		t.Fatalf("edit: %+v %v", v, err)
	}
	// While hidden, the review is deleted and posted again.
	if err = e.trust.DeleteReview(ctx, buyer, order); err != nil {
		t.Fatal(err)
	}
	// A retried delete finds its review gone; the seller has none to delete.
	if err = e.trust.DeleteReview(ctx, buyer, order); err != nil {
		t.Fatalf("delete twice: %v", err)
	}
	if err = e.trust.DeleteReview(ctx, seller, order); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the seller's delete: %v", err)
	}
	if v, err = e.trust.PutReview(ctx, buyer, order, 3, "Đăng lại", nil, nil); err != nil || v.Stars != 3 {
		t.Fatalf("post again: %+v %v", v, err)
	}
	// Hidden from the public until the seller reviews too or the window ends.
	if got, _ := e.trust.Reviews(ctx, nil, seller, domain.RoleSeller, page.Request{}); len(got.Items) != 0 {
		t.Fatalf("hidden: %+v", got)
	}
	if r, _ := e.trust.Rating(ctx, seller, domain.RoleSeller); !isRating(r, 0, 0) {
		t.Fatalf("hidden rating: %+v", r)
	}
	e.reveal(t, order)
	got, err := e.trust.Reviews(ctx, nil, seller, domain.RoleSeller, page.Request{})
	if err != nil || len(got.Items) != 1 || got.Items[0].Text != "Đăng lại" {
		t.Fatalf("%+v %v", got, err)
	}
	if r, rErr := e.trust.Rating(ctx, seller, domain.RoleSeller); rErr != nil || !isRating(r, 3, 1) {
		t.Fatalf("rating: %+v %v", r, rErr)
	}
	if r, _ := e.trust.Rating(ctx, seller, domain.RoleBuyer); !isRating(r, 0, 0) {
		t.Fatalf("buyer rating of the seller: %+v", r)
	}
	// Once it shows, it no longer changes.
	if _, err = e.trust.PutReview(ctx, buyer, order, 1, "Đổi ý", nil, nil); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("edit once shown: %v", err)
	}
	if err = e.trust.DeleteReview(ctx, buyer, order); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("delete once shown: %v", err)
	}
}

// A retry of a put or a reply whose first try landed answers the review as
// it is; a different put or reply is still refused.
func TestReviewRetries(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, order := e.completed(t, buyer, seller)
	if _, err := e.trust.PutReview(ctx, seller, order, 4, "Nhận đúng hẹn", nil, nil); err != nil {
		t.Fatal(err)
	}
	// The seller reviewed, so the buyer's review shows at once.
	tags := []domain.Tag{domain.TagAsDescribed}
	first, err := e.trust.PutReview(ctx, buyer, order, 5, "Đúng mô tả", tags, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.trust.PutReview(ctx, buyer, order, 5, " Đúng mô tả ", tags, []uuid.UUID{})
	if err != nil || again.ID != first.ID || again.Stars != 5 {
		t.Fatalf("retry: %+v %v", again, err)
	}
	if again, err = e.trust.PutReview(ctx, buyer, order, 5, "Đúng mô tả", nil, nil); err != nil || again.ID != first.ID {
		t.Fatalf("retry keeping tags: %+v %v", again, err)
	}
	for name, put := range map[string]func() error{
		"stars": func() error {
			_, perr := e.trust.PutReview(ctx, buyer, order, 4, "Đúng mô tả", tags, nil)
			return perr
		},
		"text": func() error { _, perr := e.trust.PutReview(ctx, buyer, order, 5, "Khác", tags, nil); return perr },
		"tags": func() error {
			_, perr := e.trust.PutReview(ctx, buyer, order, 5, "Đúng mô tả", []domain.Tag{}, nil)
			return perr
		},
		"photos": func() error {
			_, perr := e.trust.PutReview(ctx, buyer, order, 5, "Đúng mô tả", tags, []uuid.UUID{uuid.NewV7()})
			return perr
		},
	} {
		if perr := put(); !errors.Is(perr, domain.ErrLocked) {
			t.Errorf("another %s: %v", name, perr)
		}
	}
	replied, err := e.trust.ReplyReview(ctx, seller, first.ID, "Cảm ơn")
	if err != nil {
		t.Fatal(err)
	}
	if again, err = e.trust.ReplyReview(ctx, seller, first.ID, "Cảm ơn "); err != nil || again.Reply == nil || again.Reply.Text != replied.Reply.Text {
		t.Fatalf("reply retry: %+v %v", again, err)
	}
	if _, err = e.trust.ReplyReview(ctx, seller, first.ID, "Lần hai"); !errors.Is(err, domain.ErrReplied) {
		t.Fatalf("second reply: %v", err)
	}
	if _, err = e.trust.ReplyReview(ctx, buyer, first.ID, "Cảm ơn"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the reviewer's reply: %v", err)
	}
	var replies int
	if err = e.db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'review.replied' AND subject_id = $1`, first.ID).Scan(&replies); err != nil || replies != 1 {
		t.Fatalf("review.replied: %d %v", replies, err)
	}
}

// The second side's review shows both at once; the subject replies once,
// which closes the review.
func TestTwoSidesAndReply(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	_, order := e.completed(t, buyer, seller)
	first, err := e.trust.PutReview(ctx, buyer, order, 5, "Nhiệt tình", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.trust.ReplyReview(ctx, seller, first.ID, "Cảm ơn"); !errors.Is(err, domain.ErrHidden) {
		t.Fatalf("reply while hidden: %v", err)
	}
	m, err := e.trust.OrderReviews(ctx, seller, []uuid.UUID{order})
	if err != nil || m[order].Own.State != "none" || m[order].Their == nil || m[order].Their.State != "hidden" || m[order].Their.Stars != nil {
		t.Fatalf("seller before reviewing: %+v %v", m[order], err)
	}
	second, err := e.trust.PutReview(ctx, seller, order, 4, "Nhận hàng đúng hẹn", nil, nil)
	if err != nil || second.Role != domain.RoleBuyer || second.Reviewer.ID != seller {
		t.Fatalf("seller's review: %+v %v", second, err)
	}
	if got, _ := e.trust.Reviews(ctx, nil, buyer, domain.RoleBuyer, page.Request{}); len(got.Items) != 1 {
		t.Fatalf("buyer's reviews: %+v", got)
	}
	if r, _ := e.trust.Rating(ctx, seller, domain.RoleSeller); !isRating(r, 5, 1) {
		t.Fatalf("seller rating: %+v", r)
	}
	if r, _ := e.trust.Rating(ctx, buyer, domain.RoleBuyer); !isRating(r, 4, 1) {
		t.Fatalf("buyer rating: %+v", r)
	}
	m, err = e.trust.OrderReviews(ctx, seller, []uuid.UUID{order})
	if their := m[order].Their; err != nil || their.State != "visible" || *their.Stars != 5 || !their.CanReply || m[order].Own.State != "active" {
		t.Fatalf("seller after reviewing: %+v %v", m[order], err)
	}
	for name, who := range map[string]uuid.UUID{"the reviewer": buyer, "a stranger": stranger} {
		if _, err = e.trust.ReplyReview(ctx, who, first.ID, "Chen vào"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	replied, err := e.trust.ReplyReview(ctx, seller, first.ID, " Cảm ơn bạn ")
	if err != nil || replied.Reply == nil || replied.Reply.Text != "Cảm ơn bạn" {
		t.Fatalf("reply: %+v %v", replied, err)
	}
	if _, err = e.trust.ReplyReview(ctx, seller, first.ID, "Lần hai"); !errors.Is(err, domain.ErrReplied) {
		t.Fatalf("second reply: %v", err)
	}
	if _, err = e.trust.PutReview(ctx, buyer, order, 1, "Đổi ý", nil, nil); !errors.Is(err, domain.ErrClosed) {
		t.Fatalf("edit after the reply: %v", err)
	}
	if err = e.trust.DeleteReview(ctx, buyer, order); !errors.Is(err, domain.ErrClosed) {
		t.Fatalf("delete after the reply: %v", err)
	}
	m, err = e.trust.OrderReviews(ctx, buyer, []uuid.UUID{order})
	if own := m[order].Own; err != nil || !own.Locked || own.Reply == nil || own.Reply.Text != "Cảm ơn bạn" {
		t.Fatalf("buyer's own review: %+v %v", own, err)
	}
	var replies int
	if err = e.db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'review.replied' AND subject_id = $1`, first.ID).Scan(&replies); err != nil || replies != 1 {
		t.Fatalf("review.replied: %d %v", replies, err)
	}
}

func TestPutReviewRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	_, completed := e.completed(t, buyer, seller)
	if _, err := e.trust.PutReview(ctx, stranger, completed, 5, "", nil, nil); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Errorf("a stranger: %v", err)
	}
	if _, err := e.trust.PutReview(ctx, buyer, uuid.NewV7(), 5, "", nil, nil); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Errorf("unknown order: %v", err)
	}
	_, shipped := e.shipped(t, buyer, seller)
	for name, who := range map[string]uuid.UUID{"buyer": buyer, "seller": seller} {
		if _, err := e.trust.PutReview(ctx, who, shipped, 5, "", nil, nil); !errors.Is(err, domain.ErrNotDelivered) {
			t.Errorf("shipped, %s: %v", name, err)
		}
	}
	deal, requested := e.pending(t, buyer, seller)
	if _, err := e.trust.PutReview(ctx, buyer, requested, 5, "", nil, nil); !errors.Is(err, domain.ErrNotDelivered) {
		t.Errorf("pending: %v", err)
	}
	if _, err := e.deals.Cancel(ctx, buyer, deal); err != nil {
		t.Fatal(err)
	}
	if _, err := e.trust.PutReview(ctx, buyer, requested, 5, "", nil, nil); !errors.Is(err, domain.ErrNotDelivered) {
		t.Errorf("cancelled: %v", err)
	}
	if _, err := e.trust.PutReview(ctx, buyer, completed, 9, "", nil, nil); !errors.Is(err, domain.ErrInvalidReview) {
		t.Errorf("nine stars: %v", err)
	}
	accounttest.Suspend(t, e.accounts, buyer)
	if _, err := e.trust.PutReview(ctx, buyer, completed, 5, "", nil, nil); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Errorf("suspended: %v", err)
	}
	if err := e.trust.DeleteReview(ctx, buyer, completed); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Errorf("suspended delete: %v", err)
	}
	if err := e.trust.UnmarkHelpful(ctx, buyer, uuid.NewV7()); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Errorf("suspended unmark: %v", err)
	}
}

func TestReviewsOfAStranger(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, err := e.trust.Reviews(context.Background(), nil, uuid.NewV7(), domain.RoleSeller, page.Request{}); !errors.Is(err, accountdomain.ErrAccountNotFound) {
		t.Fatalf("%v", err)
	}
}

// A failed delivery is reviewed by both sides: a refused parcel is the
// seller's case.
func TestFailedOrderIsReviewed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, order := e.shipped(t, buyer, seller)
	f, _, err := e.deals.Order(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.deals.ShipmentUpdated(ctx, string(f.Shipment.Carrier), f.Shipment.TrackingCode, "failed", time.Time{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.trust.PutReview(ctx, seller, order, 1, "Không nhận hàng", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.trust.PutReview(ctx, buyer, order, 3, "", nil, nil); err != nil {
		t.Fatal(err)
	}
}
