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
	_, order := e.completed(t, buyer, seller)
	first, err := e.trust.PutReview(ctx, buyer, order, 5, "Tốt")
	if err != nil {
		t.Fatal(err)
	}
	f, ok, err := e.trust.ReviewFacts(ctx, first.ID)
	if err != nil || !ok || f.SubjectID != seller || f.ReviewerID != buyer || f.OrderID != order || f.Stars != 5 ||
		f.ReviewerName == "" || f.ListingTitle == "" || f.HiddenFor <= 0 || f.Revealed != (uuid.UUID{}) {
		t.Fatalf("first: %+v %v %v", f, ok, err)
	}
	second, err := e.trust.PutReview(ctx, seller, order, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	g, ok, err := e.trust.ReviewFacts(ctx, second.ID)
	if err != nil || !ok || g.SubjectID != buyer || g.HiddenFor != 0 || g.Revealed != first.ID {
		t.Fatalf("second: %+v %v %v", g, ok, err)
	}
	if f, _, _ = e.trust.ReviewFacts(ctx, first.ID); f.HiddenFor != 0 || !f.VisibleAt.Equal(g.VisibleAt) || f.RevealedBy != second.ID {
		t.Fatalf("first after the second: %+v", f)
	}
	if _, ok, err = e.trust.ReviewFacts(ctx, uuid.NewV7()); err != nil || ok {
		t.Fatalf("an unknown review: %v %v", ok, err)
	}
}
