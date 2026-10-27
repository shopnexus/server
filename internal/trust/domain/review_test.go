package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/trust/domain"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func newReview(stars int, text string) (domain.Review, error) {
	return domain.NewReview(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), stars, text, now)
}

func TestNewReview(t *testing.T) {
	t.Parallel()
	r, err := newReview(5, "  Đúng mô tả  ")
	if err != nil || r.Stars() != 5 || r.Text() != "Đúng mô tả" || !r.CreatedAt().Equal(now) {
		t.Fatalf("%+v %v", r.State(), err)
	}
	if _, err = newReview(4, ""); err != nil {
		t.Fatalf("no text: %v", err)
	}
	for name, c := range map[string]struct {
		stars int
		text  string
		field string
	}{
		"no stars":  {0, "", "stars"},
		"six stars": {6, "", "stars"},
		"long text": {3, strings.Repeat("a", 501), "text"},
	} {
		_, err := newReview(c.stars, c.text)
		if e, ok := errors.AsType[*apperr.Error](err); !ok || !errors.Is(err, domain.ErrInvalidReview) || e.Fields[c.field] == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestEditWindow(t *testing.T) {
	t.Parallel()
	r, err := newReview(5, "Tốt")
	if err != nil {
		t.Fatal(err)
	}
	day := 24 * time.Hour
	if err = r.Edit(2, "Hỏng sau 3 ngày", now.Add(6*day)); err != nil || r.Stars() != 2 || !r.UpdatedAt().Equal(now.Add(6*day)) {
		t.Fatalf("day 6: %+v %v", r.State(), err)
	}
	if !r.CreatedAt().Equal(now) {
		t.Fatal("an edit moved the first submission")
	}
	if err = r.Edit(1, "", now.Add(8*day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("day 8: %v", err)
	}
	if err = r.CanChange(now.Add(8 * day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("delete day 8: %v", err)
	}
	if err = r.CanChange(now.Add(7 * day)); err != nil {
		t.Fatalf("day 7: %v", err)
	}
}

func TestDeleteAndRepost(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	r, err := newReview(5, "Tốt")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Delete(now.Add(2 * day)); err != nil || !r.Deleted() || r.Text() != "" {
		t.Fatalf("delete: %+v %v", r.State(), err)
	}
	if err = r.Edit(3, "Đăng lại", now.Add(5*day)); err != nil || r.Deleted() || r.Stars() != 3 || !r.CreatedAt().Equal(now) {
		t.Fatalf("repost on day 5: %+v %v", r.State(), err)
	}
	if err = r.Delete(now.Add(6 * day)); err != nil {
		t.Fatal(err)
	}
	if err = r.Edit(1, "Lần nữa", now.Add(8*day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("repost on day 8: %v", err)
	}
	if err = r.Delete(now.Add(8 * day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("delete on day 8: %v", err)
	}
}
