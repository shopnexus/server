package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"uuid"

	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/trust/domain"
)

// A review's photos are processed when it is written and keep their order;
// leaving them out keeps them, [] removes them, and another person's upload
// is refused.
// UC-TRS-01 E3.
func TestReviewPhotos(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, order := e.completed(t, buyer, seller)
	upload := func(owner uuid.UUID) uuid.UUID {
		return mediatest.Upload(t, e.media, owner, "image/jpeg", mediatest.JPEG(t, 800, 600))
	}
	a, b := upload(buyer), upload(buyer)

	v, err := e.trust.PutReview(ctx, buyer, order, 5, "Như hình", nil, []uuid.UUID{b, a})
	if err != nil || len(v.Photos) != 2 || v.Photos[0].UploadID != b || v.Photos[1].UploadID != a ||
		!strings.Contains(v.Photos[0].URL, "media/") || v.Photos[0].ThumbURL == v.Photos[0].URL {
		t.Fatalf("%+v %v", v.Photos, err)
	}
	if v, err = e.trust.PutReview(ctx, buyer, order, 4, "Vẫn như hình", nil, nil); err != nil || len(v.Photos) != 2 {
		t.Fatalf("left out: %+v %v", v.Photos, err)
	}
	c := upload(buyer)
	if v, err = e.trust.PutReview(ctx, buyer, order, 4, "", nil, []uuid.UUID{a, c}); err != nil || len(v.Photos) != 2 || v.Photos[1].UploadID != c {
		t.Fatalf("one kept, one new: %+v %v", v.Photos, err)
	}
	m, err := e.trust.OrderReviews(ctx, buyer, []uuid.UUID{order})
	if err != nil || len(m[order].Own.Photos) != 2 {
		t.Fatalf("own: %+v %v", m[order].Own, err)
	}
	if _, err = e.trust.PutReview(ctx, buyer, order, 4, "", nil, []uuid.UUID{upload(seller)}); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("someone else's: %v", err)
	}
	if v, err = e.trust.PutReview(ctx, buyer, order, 4, "", nil, []uuid.UUID{}); err != nil || len(v.Photos) != 0 {
		t.Fatalf("removed: %+v %v", v.Photos, err)
	}
	if _, err = e.trust.PutReview(ctx, buyer, order, 4, "", nil, make([]uuid.UUID, domain.MaxPhotos+1)); err == nil {
		t.Fatal("five photos")
	}
}
