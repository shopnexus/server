package service_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/trust/service"
)

// The rating counts every review of the seller; a seller without reviews
// has none.
func TestRating(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	for _, stars := range []int{4, 5} {
		_, order := e.shipped(t, buyer, seller)
		if _, err := e.trust.PutReview(ctx, buyer, order, stars, ""); err != nil {
			t.Fatal(err)
		}
	}
	if r, err := e.trust.Rating(ctx, seller); err != nil || r != (service.Rating{Average: 4.5, Count: 2}) {
		t.Fatalf("%+v %v", r, err)
	}
	if r, err := e.trust.Rating(ctx, uuid.NewV7()); err != nil || r != (service.Rating{}) {
		t.Fatalf("no reviews: %+v %v", r, err)
	}
}
