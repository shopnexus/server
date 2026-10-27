package domain_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/media/domain"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func TestNewUploadLimits(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		contentType string
		size        int64
		ok          bool
	}{
		"jpeg":        {"image/jpeg", 1, true},
		"png at 15MB": {"image/png", domain.MaxSize, true},
		"webp":        {"image/webp", 10, true},
		"heic":        {"image/heic", 10, false},
		"empty":       {"image/jpeg", 0, false},
		"over 15MB":   {"image/jpeg", domain.MaxSize + 1, false},
	} {
		_, err := domain.NewUpload(uuid.NewV7(), uuid.NewV7(), c.contentType, c.size, now)
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, domain.ErrInvalidUpload)) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Only the owner attaches a waiting upload. Attaching it again to the same
// holder changes nothing, so a retried attach succeeds; another holder is
// refused.
func TestAttachOnlyByTheOwnerWhileWaiting(t *testing.T) {
	t.Parallel()
	owner, holder := uuid.NewV7(), uuid.NewV7()
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, now)
	if _, err := u.Attach(uuid.NewV7(), holder, domain.ShapePhoto, now); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("another owner: %v", err)
	}
	if changed, err := u.Attach(owner, holder, domain.ShapePhoto, now); err != nil || !changed || u.Status() != domain.StatusAttached ||
		u.HolderID() != holder || u.Shape() != domain.ShapePhoto {
		t.Fatalf("owner: %v %v %s", changed, err, u.Status())
	}
	if changed, err := u.Attach(owner, holder, domain.ShapePhoto, now); err != nil || changed {
		t.Fatalf("again to the same holder: %v %v", changed, err)
	}
	u.Processed("media/a.jpg", "media/a_thumb.jpg", now)
	if changed, err := u.Attach(owner, holder, domain.ShapePhoto, now); err != nil || changed || u.Status() != domain.StatusReady {
		t.Fatalf("processed, same holder: %v %v %s", changed, err, u.Status())
	}
	if _, err := u.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("another holder: %v", err)
	}
}

// An upload made a photo for a holder is not made a square for it too.
func TestAttachKeepsItsShape(t *testing.T) {
	t.Parallel()
	owner, holder := uuid.NewV7(), uuid.NewV7()
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, now)
	if _, err := u.Attach(owner, holder, domain.ShapeSquare, now); err != nil || u.Shape() != domain.ShapeSquare {
		t.Fatalf("square: %v %s", err, u.Shape())
	}
	if changed, err := u.Attach(owner, holder, domain.ShapeSquare, now); err != nil || changed {
		t.Fatalf("square again: %v %v", changed, err)
	}
	if _, err := u.Attach(owner, holder, domain.ShapePhoto, now); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("as a photo: %v", err)
	}
}

func TestExpireOnlyWhileWaiting(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	waiting, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, now)
	if !waiting.Expire(now) || waiting.Status() != domain.StatusExpired {
		t.Fatalf("waiting: %s", waiting.Status())
	}
	attached, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, now)
	_, _ = attached.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now)
	if attached.Expire(now) || attached.Status() != domain.StatusAttached {
		t.Fatalf("attached: %s", attached.Status())
	}
}

func TestProcessedAndFailed(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, now)
	_, _ = u.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now)
	u.Processed("media/a.jpg", "media/a_thumb.jpg", now)
	if u.Status() != domain.StatusReady || u.PhotoKey() != "media/a.jpg" || u.ThumbKey() != "media/a_thumb.jpg" {
		t.Fatalf("%+v", u.State())
	}
	f, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, now)
	_, _ = f.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now)
	f.Failed("not an image", now)
	if f.Status() != domain.StatusFailed || f.Failure() != "not an image" {
		t.Fatalf("%+v", f.State())
	}
}

// A Restate step returns an Upload; it survives the journal.
func TestUploadJSONRoundTrip(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/png", 42, now)
	_, _ = u.Attach(owner, uuid.NewV7(), domain.ShapeSquare, now)
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var back domain.Upload
	if err := json.Unmarshal(b, &back); err != nil || back.State() != u.State() {
		t.Fatalf("%v\n%+v\n%+v", err, back.State(), u.State())
	}
}
