package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"

	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/service"
)

// The rating counts the visible reviews of a person in one role; someone
// without reviews has none.
func TestRating(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	for _, stars := range []int{4, 5} {
		_, order := e.completed(t, buyer, seller)
		if _, err := e.trust.PutReview(ctx, buyer, order, stars, "", nil, nil); err != nil {
			t.Fatal(err)
		}
		e.reveal(t, order)
	}
	if r, err := e.trust.Rating(ctx, seller, domain.RoleSeller); err != nil || !isRating(r, 4.5, 2) {
		t.Fatalf("%+v %v", r, err)
	}
	if r, err := e.trust.Rating(ctx, uuid.NewV7(), domain.RoleSeller); err != nil || !isRating(r, 0, 0) {
		t.Fatalf("no reviews: %+v %v", r, err)
	}
}

// isRating reports whether r has the average, count and tags given, with
// tags [] rather than nil.
func isRating(r service.Rating, average float64, count int, tags ...service.TagCount) bool {
	return r.Average == average && r.Count == count && r.Tags != nil && slices.Equal(r.Tags, tags)
}

// A review's tags fit its role; an edit without them keeps them, [] clears
// them, and the rating counts them over the visible reviews, most used first.
func TestRatingTags(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	tags := [][]domain.Tag{
		{domain.TagQuickReply, domain.TagWellPacked},
		{domain.TagWellPacked},
		{domain.TagAsDescribed, domain.TagWellPacked},
	}
	orders := make([]uuid.UUID, 0, len(tags))
	for _, tt := range tags {
		_, order := e.completed(t, buyer, seller)
		v, err := e.trust.PutReview(ctx, buyer, order, 5, "", tt, nil)
		if err != nil || !slices.Equal(v.Tags, tt) {
			t.Fatalf("%+v %v", v, err)
		}
		orders = append(orders, order)
	}
	v, err := e.trust.PutReview(ctx, buyer, orders[0], 5, "Sửa", nil, nil)
	if err != nil || !slices.Equal(v.Tags, tags[0]) {
		t.Fatalf("edit keeps the tags: %+v %v", v, err)
	}
	if v, err = e.trust.PutReview(ctx, buyer, orders[1], 5, "", []domain.Tag{}, nil); err != nil || v.Tags == nil || len(v.Tags) != 0 {
		t.Fatalf("[] clears the tags: %+v %v", v, err)
	}
	for _, bad := range [][]domain.Tag{{domain.TagOnTime}, {"friendly"}, {domain.TagWellPacked, domain.TagWellPacked}} {
		_, err = e.trust.PutReview(ctx, buyer, orders[2], 5, "", bad, nil)
		if ae, ok := errors.AsType[*apperr.Error](err); !ok || !errors.Is(err, domain.ErrInvalidReview) || ae.Fields["tags"] == "" {
			t.Fatalf("%v: %v", bad, err)
		}
	}
	if r, _ := e.trust.Rating(ctx, seller, domain.RoleSeller); !isRating(r, 0, 0) {
		t.Fatalf("hidden: %+v", r)
	}
	for _, o := range orders {
		e.reveal(t, o)
	}
	want := []service.TagCount{{Tag: domain.TagWellPacked, Count: 2}, {Tag: domain.TagAsDescribed, Count: 1}, {Tag: domain.TagQuickReply, Count: 1}}
	if r, ratingErr := e.trust.Rating(ctx, seller, domain.RoleSeller); ratingErr != nil || !isRating(r, 5, 3, want...) {
		t.Fatalf("%+v %v", r, ratingErr)
	}
	m, err := e.trust.OrderReviews(ctx, seller, orders[:1])
	if their := m[orders[0]].Their; err != nil || their == nil || !slices.Equal(their.Tags, tags[0]) {
		t.Fatalf("their review: %+v %v", m, err)
	}
	if m, err = e.trust.OrderReviews(ctx, buyer, orders[:1]); err != nil || !slices.Equal(m[orders[0]].Own.Tags, tags[0]) {
		t.Fatalf("own review: %+v %v", m, err)
	}
}
