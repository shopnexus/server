package service_test

import (
	"context"
	"testing"
	"uuid"
)

func TestReviewFacts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, order := e.shipped(t, buyer, seller)
	r, err := e.trust.PutReview(ctx, buyer, order, 5, "Tốt")
	if err != nil {
		t.Fatal(err)
	}
	f, ok, err := e.trust.ReviewFacts(ctx, r.ID)
	if err != nil || !ok || f.SellerID != seller || f.Stars != 5 || f.BuyerName == "" || f.ListingTitle == "" {
		t.Fatalf("%+v %v %v", f, ok, err)
	}
	if _, ok, err = e.trust.ReviewFacts(ctx, uuid.NewV7()); err != nil || ok {
		t.Fatalf("an unknown review: %v %v", ok, err)
	}
}
