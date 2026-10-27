package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/trust/domain"
)

func TestPutReview(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, order := e.shipped(t, buyer, seller)
	v, err := e.trust.PutReview(ctx, buyer, order, 5, "Đúng mô tả")
	if err != nil || v.Stars != 5 || v.Buyer.ID != buyer || v.Buyer.DisplayName != "Linh" || v.Listing.Title == "" {
		t.Fatalf("%+v %v", v, err)
	}
	if v, err = e.trust.PutReview(ctx, buyer, order, 4, "Hơi trầy"); err != nil || v.Stars != 4 {
		t.Fatalf("edit: %+v %v", v, err)
	}
	got, err := e.trust.SellerReviews(ctx, seller, page.Request{})
	if err != nil || got.Count != 1 || got.Average != 4 || len(got.Items) != 1 || got.Items[0].Text != "Hơi trầy" {
		t.Fatalf("%+v %v", got, err)
	}
	if err = e.trust.DeleteReview(ctx, buyer, order); err != nil {
		t.Fatal(err)
	}
	if err = e.trust.DeleteReview(ctx, buyer, order); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if v, err = e.trust.PutReview(ctx, buyer, order, 3, "Đăng lại"); err != nil || v.Stars != 3 {
		t.Fatalf("post again: %+v %v", v, err)
	}
	if got, _ = e.trust.SellerReviews(ctx, seller, page.Request{}); got.Count != 1 || got.Average != 3 {
		t.Fatalf("after posting again: %+v", got)
	}
}

func TestPutReviewRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, stranger := e.person(t), e.person(t), e.person(t)
	_, shipped := e.shipped(t, buyer, seller)
	for name, who := range map[string]uuid.UUID{"a stranger": stranger, "the seller": seller} {
		if _, err := e.trust.PutReview(ctx, who, shipped, 5, ""); !errors.Is(err, domain.ErrOrderNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.trust.PutReview(ctx, buyer, uuid.NewV7(), 5, ""); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Errorf("unknown order: %v", err)
	}
	deal, requested := e.requested(t, buyer, seller)
	if _, err := e.trust.PutReview(ctx, buyer, requested, 5, ""); !errors.Is(err, domain.ErrNotShipped) {
		t.Errorf("requested: %v", err)
	}
	if _, err := e.deals.Cancel(ctx, buyer, deal); err != nil {
		t.Fatal(err)
	}
	if _, err := e.trust.PutReview(ctx, buyer, requested, 5, ""); !errors.Is(err, domain.ErrNotShipped) {
		t.Errorf("cancelled: %v", err)
	}
	if _, err := e.trust.PutReview(ctx, buyer, shipped, 9, ""); !errors.Is(err, domain.ErrInvalidReview) {
		t.Errorf("nine stars: %v", err)
	}
	e.suspend(t, buyer)
	if _, err := e.trust.PutReview(ctx, buyer, shipped, 5, ""); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Errorf("suspended: %v", err)
	}
}

func TestReturnedOrderIsReviewed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	deal, order := e.shipped(t, buyer, seller)
	if _, err := e.deals.Return(ctx, seller, deal, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.trust.PutReview(ctx, buyer, order, 1, "Không đúng mô tả"); err != nil {
		t.Fatal(err)
	}
}

func TestSellerReviewsOfAStranger(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, err := e.trust.SellerReviews(context.Background(), uuid.NewV7(), page.Request{}); !errors.Is(err, accountdomain.ErrAccountNotFound) {
		t.Fatalf("%v", err)
	}
}
