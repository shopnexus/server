package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/service"
)

// Anyone but the reviewer and the subject marks a visible review helpful,
// once; a reader sees their own mark, and the count holds every mark.
// UC-TRS-05 A1, E2, E3.
func TestReviewHelpful(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer, reader, other := e.person(t), e.person(t), e.person(t), e.person(t)
	_, order := e.completed(t, buyer, seller)
	v, err := e.trust.PutReview(ctx, buyer, order, 5, "Tốt", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Helpful != (service.Helpful{}) {
		t.Fatalf("the reviewer's view: %+v", v.Helpful)
	}
	if err = e.trust.MarkHelpful(ctx, reader, v.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("hidden: %v", err)
	}
	e.reveal(t, order)
	for _, own := range []uuid.UUID{buyer, seller} {
		if err = e.trust.MarkHelpful(ctx, own, v.ID); !errors.Is(err, domain.ErrOwnReview) {
			t.Fatalf("own: %v", err)
		}
	}
	for range 2 {
		if err = e.trust.MarkHelpful(ctx, reader, v.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err = e.trust.MarkHelpful(ctx, other, v.ID); err != nil {
		t.Fatal(err)
	}
	helpful := func(viewer *uuid.UUID) service.Helpful {
		t.Helper()
		got, listErr := e.trust.Reviews(ctx, viewer, seller, domain.RoleSeller, page.Request{})
		if listErr != nil || len(got.Items) != 1 {
			t.Fatalf("%+v %v", got, listErr)
		}
		return got.Items[0].Helpful
	}
	if h := helpful(&reader); h.Count != 2 || h.Marked == nil || !*h.Marked {
		t.Fatalf("the reader: %+v", h)
	}
	if h := helpful(nil); h.Count != 2 || h.Marked != nil {
		t.Fatalf("signed out: %+v", h)
	}
	if h := helpful(&seller); h.Count != 2 || h.Marked != nil {
		t.Fatalf("the subject: %+v", h)
	}
	for range 2 {
		if err = e.trust.UnmarkHelpful(ctx, reader, v.ID); err != nil {
			t.Fatal(err)
		}
	}
	if h := helpful(&reader); h.Count != 1 || h.Marked == nil || *h.Marked {
		t.Fatalf("unmarked: %+v", h)
	}
}
