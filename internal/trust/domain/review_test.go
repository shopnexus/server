package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/trust/domain"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

var subject = uuid.NewV7()

// newReview is a first review still hidden: the other side has not
// reviewed, and it shows 10 days on, after the edit window.
func newReview(stars int, text string) (domain.Review, error) {
	return domain.NewReview(uuid.NewV7(), uuid.NewV7(), domain.RoleSeller, uuid.NewV7(), subject, uuid.NewV7(), stars, text, nil, nil,
		now.Add(10*24*time.Hour), now)
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

// A review carries distinct tags of its role; a delete drops them.
func TestTags(t *testing.T) {
	t.Parallel()
	review := func(role domain.Role, tags ...domain.Tag) (domain.Review, error) {
		return domain.NewReview(uuid.NewV7(), uuid.NewV7(), role, uuid.NewV7(), subject, uuid.NewV7(), 5, "", tags, nil,
			now.Add(10*24*time.Hour), now)
	}
	r, err := review(domain.RoleSeller, domain.TagWellPacked, domain.TagAsDescribed)
	if err != nil || !slices.Equal(r.Tags(), []domain.Tag{domain.TagWellPacked, domain.TagAsDescribed}) {
		t.Fatalf("%+v %v", r.State(), err)
	}
	if _, err = review(domain.RoleBuyer, domain.TagOnTime, domain.TagQuickReply); err != nil {
		t.Fatalf("buyer tags: %v", err)
	}
	for name, c := range map[string]struct {
		role domain.Role
		tags []domain.Tag
	}{
		"unknown":    {domain.RoleSeller, []domain.Tag{"friendly"}},
		"other role": {domain.RoleSeller, []domain.Tag{domain.TagOnTime}},
		"buyer":      {domain.RoleBuyer, []domain.Tag{domain.TagWellPacked}},
		"twice":      {domain.RoleSeller, []domain.Tag{domain.TagQuickReply, domain.TagQuickReply}},
	} {
		_, tagErr := review(c.role, c.tags...)
		if e, ok := errors.AsType[*apperr.Error](tagErr); !ok || !errors.Is(tagErr, domain.ErrInvalidReview) || e.Fields["tags"] == "" {
			t.Errorf("%s: %v", name, tagErr)
		}
	}
	if err = r.Edit(4, "", []domain.Tag{domain.TagQuickReply}, nil, now.Add(time.Hour)); err != nil || !slices.Equal(r.Tags(), []domain.Tag{domain.TagQuickReply}) {
		t.Fatalf("edit: %+v %v", r.State(), err)
	}
	if err = r.Delete(now.Add(2 * time.Hour)); err != nil || len(r.Tags()) != 0 {
		t.Fatalf("delete: %+v %v", r.State(), err)
	}
}

func TestEditWindow(t *testing.T) {
	t.Parallel()
	r, err := newReview(5, "Tốt")
	if err != nil {
		t.Fatal(err)
	}
	day := 24 * time.Hour
	if err = r.Edit(2, "Hỏng sau 3 ngày", nil, nil, now.Add(6*day)); err != nil || r.Stars() != 2 || !r.UpdatedAt().Equal(now.Add(6*day)) {
		t.Fatalf("day 6: %+v %v", r.State(), err)
	}
	if !r.CreatedAt().Equal(now) {
		t.Fatal("an edit moved the first submission")
	}
	if err = r.Edit(1, "", nil, nil, now.Add(8*day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("day 8: %v", err)
	}
	if err = r.CanChange(now.Add(8 * day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("delete day 8: %v", err)
	}
	if err = r.CanChange(now.Add(7 * day)); err != nil {
		t.Fatalf("day 7: %v", err)
	}
}

func TestLockedOnceShown(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	r, err := domain.NewReview(uuid.NewV7(), uuid.NewV7(), domain.RoleSeller, uuid.NewV7(), subject, uuid.NewV7(), 4, "Ổn", nil, nil,
		now.Add(3*day), now)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.ChangeUntil(); !got.Equal(now.Add(3 * day)) {
		t.Fatalf("change until %v, want when it shows", got)
	}
	if err = r.Edit(5, "Tốt", nil, nil, now.Add(2*day)); err != nil {
		t.Fatalf("hidden on day 2: %v", err)
	}
	if err = r.Edit(1, "Tệ", nil, nil, now.Add(3*day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("shown on day 3: %v", err)
	}
	if err = r.Delete(now.Add(4 * day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("delete once shown: %v", err)
	}
	hidden, err := newReview(5, "Tốt")
	if err != nil {
		t.Fatal(err)
	}
	if got := hidden.ChangeUntil(); !got.Equal(now.Add(domain.EditWindow)) {
		t.Fatalf("change until %v, want the end of the window", got)
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
	if err = r.Edit(3, "Đăng lại", nil, nil, now.Add(5*day)); err != nil || r.Deleted() || r.Stars() != 3 || !r.CreatedAt().Equal(now) {
		t.Fatalf("repost on day 5: %+v %v", r.State(), err)
	}
	if err = r.Delete(now.Add(6 * day)); err != nil {
		t.Fatal(err)
	}
	if err = r.Edit(1, "Lần nữa", nil, nil, now.Add(8*day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("repost on day 8: %v", err)
	}
	if err = r.Delete(now.Add(8 * day)); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("delete on day 8: %v", err)
	}
}

func TestRevealAt(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	closed := now.Add(-2 * day)
	if got := domain.RevealAt(closed, false, now); !got.Equal(closed.Add(7 * day)) {
		t.Errorf("first side: %v", got)
	}
	if got := domain.RevealAt(closed, true, now); !got.Equal(now) {
		t.Errorf("second side: %v", got)
	}
	if got := domain.RevealAt(now.Add(-9*day), false, now); !got.Equal(now) {
		t.Errorf("after the window: %v", got)
	}
}

func TestReply(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	r, err := domain.NewReview(uuid.NewV7(), uuid.NewV7(), domain.RoleBuyer, uuid.NewV7(), subject, uuid.NewV7(), 2, "Bom hàng", nil, nil,
		now.Add(3*day), now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Visible(now) || !r.Visible(now.Add(3*day)) {
		t.Fatal("visibility")
	}
	if err = r.Reply(subject, "Xin lỗi", now.Add(day)); !errors.Is(err, domain.ErrHidden) {
		t.Fatalf("hidden: %v", err)
	}
	if err = r.Reply(r.ReviewerID(), "Tự trả lời", now.Add(4*day)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("reviewer: %v", err)
	}
	if err = r.Reply(subject, "  ", now.Add(4*day)); !errors.Is(err, domain.ErrInvalidReview) {
		t.Fatalf("empty: %v", err)
	}
	if err = r.Reply(subject, strings.Repeat("a", 501), now.Add(4*day)); !errors.Is(err, domain.ErrInvalidReview) {
		t.Fatalf("long: %v", err)
	}
	if err = r.Reply(subject, " Tôi đi công tác ", now.Add(4*day)); err != nil || r.ReplyText() != "Tôi đi công tác" || !r.Replied() {
		t.Fatalf("reply: %+v %v", r.State(), err)
	}
	if err = r.Reply(subject, "Lần hai", now.Add(4*day)); !errors.Is(err, domain.ErrReplied) {
		t.Fatalf("second reply: %v", err)
	}
	if err = r.Edit(5, "Đổi ý", nil, nil, now.Add(5*day)); !errors.Is(err, domain.ErrClosed) {
		t.Fatalf("edit after reply: %v", err)
	}
	if err = r.Delete(now.Add(5 * day)); !errors.Is(err, domain.ErrClosed) {
		t.Fatalf("delete after reply: %v", err)
	}
}

func TestReplyToDeleted(t *testing.T) {
	t.Parallel()
	r, err := newReview(1, "Tệ")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Delete(now); err != nil {
		t.Fatal(err)
	}
	if err = r.Reply(subject, "Sao xoá", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}
