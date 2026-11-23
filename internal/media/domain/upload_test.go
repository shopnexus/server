package domain_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/media/domain"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func TestNewUploadLimits(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		contentType string
		size        int64
		ok          bool
	}{
		"jpeg":            {"image/jpeg", 1, true},
		"png at 15MB":     {"image/png", domain.MaxPhotoBytes, true},
		"webp":            {"image/webp", 10, true},
		"heic":            {"image/heic", 10, false},
		"empty":           {"image/jpeg", 0, false},
		"photo over 15MB": {"image/jpeg", domain.MaxPhotoBytes + 1, false},
		"mp4 at 50MB":     {"video/mp4", domain.MaxVideoBytes, true},
		"quicktime":       {"video/quicktime", 20 << 20, true},
		"mp4 over 50MB":   {"video/mp4", domain.MaxVideoBytes + 1, false},
		"webm":            {"video/webm", 10, false},
	} {
		_, err := domain.NewUpload(uuid.NewV7(), uuid.NewV7(), c.contentType, c.size, domain.OriginApp, now)
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, domain.ErrInvalidUpload)) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A video is held only as a video, and a photo never as one.
func TestAttachMatchesTheType(t *testing.T) {
	t.Parallel()
	owner, holder := uuid.NewV7(), uuid.NewV7()
	video, _ := domain.NewUpload(uuid.NewV7(), owner, domain.ContentTypeMP4, 10, domain.OriginApp, now)
	for _, shape := range []domain.Shape{domain.ShapePhoto, domain.ShapeSquare} {
		if _, err := video.Attach(owner, holder, shape, now); !errors.Is(err, domain.ErrInvalidUpload) {
			t.Fatalf("a video as %s: %v", shape, err)
		}
	}
	photo, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
	if _, err := photo.Attach(owner, holder, domain.ShapeVideo, now); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a photo as a video: %v", err)
	}
	if changed, err := video.Attach(owner, holder, domain.ShapeVideo, now); err != nil || !changed ||
		video.Status() != domain.StatusAttached || video.Shape() != domain.ShapeVideo {
		t.Fatalf("a video as a video: %v %v %+v", changed, err, video.State())
	}
	if changed, err := video.Attach(owner, holder, domain.ShapeVideo, now); err != nil || changed {
		t.Fatalf("held again by the same holder: %v %v", changed, err)
	}
	if !domain.IsVideo(domain.ContentTypeQuickTime) || domain.IsVideo("image/png") {
		t.Fatal("IsVideo")
	}
}

// A video waiting past its expiry is not held, even before MediaExpire runs.
func TestAttachRefusesAnExpiredVideo(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	video, _ := domain.NewUpload(uuid.NewV7(), owner, domain.ContentTypeMP4, 10, domain.OriginApp, now)
	if _, err := video.Attach(owner, uuid.NewV7(), domain.ShapeVideo, now.Add(domain.ExpireAfter)); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("held at its expiry: %v", err)
	}
}

// Only the owner attaches a waiting upload. Attaching it again to the same
// holder changes nothing, so a retried attach succeeds; another holder is
// refused.
func TestAttachOnlyByTheOwnerWhileWaiting(t *testing.T) {
	t.Parallel()
	owner, holder := uuid.NewV7(), uuid.NewV7()
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
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
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
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

// An upload expires once it is past its expiry, and only while it waits.
func TestExpireOnlyWhileWaitingPastItsExpiry(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	waiting, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
	if !waiting.ExpiresAt().Equal(now.Add(domain.ExpireAfter)) {
		t.Fatalf("expires %v", waiting.ExpiresAt())
	}
	if waiting.Expire(now.Add(domain.ExpireAfter-time.Second)) || waiting.Status() != domain.StatusWaiting {
		t.Fatalf("before its expiry: %s", waiting.Status())
	}
	if !waiting.Expire(now.Add(domain.ExpireAfter)) || waiting.Status() != domain.StatusExpired {
		t.Fatalf("waiting: %s", waiting.Status())
	}
	attached, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
	_, _ = attached.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now)
	if attached.Expire(now.Add(domain.ExpireAfter)) || attached.Status() != domain.StatusAttached {
		t.Fatalf("attached: %s", attached.Status())
	}
}

func TestProcessedAndFailed(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
	_, _ = u.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now)
	u.Processed("media/a.jpg", "media/a_thumb.jpg", now)
	if u.Status() != domain.StatusReady || u.PhotoKey() != "media/a.jpg" || u.ThumbKey() != "media/a_thumb.jpg" {
		t.Fatalf("%+v", u.State())
	}
	f, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
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
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/png", 42, domain.OriginApp, now)
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

// Only a held video is consumed, once.
func TestConsume(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	video, _ := domain.NewUpload(uuid.NewV7(), owner, domain.ContentTypeMP4, 10, domain.OriginApp, now)
	if video.Consume(now) {
		t.Fatal("a waiting video was consumed")
	}
	_, _ = video.Attach(owner, uuid.NewV7(), domain.ShapeVideo, now)
	if !video.Consume(now) || video.Status() != domain.StatusConsumed {
		t.Fatalf("held: %s", video.Status())
	}
	if video.Consume(now) {
		t.Fatal("consumed twice")
	}
	photo, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
	_, _ = photo.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now)
	if photo.Consume(now) || photo.Status() != domain.StatusAttached {
		t.Fatalf("a photo: %s", photo.Status())
	}
	if domain.UploadKey(video.ID()) != video.Key() {
		t.Fatal(domain.UploadKey(video.ID()))
	}
}

func TestNewUploadOrigin(t *testing.T) {
	t.Parallel()
	for _, o := range []domain.Origin{domain.OriginApp, domain.OriginFrame, domain.OriginLink, domain.OriginListing} {
		u, err := domain.NewUpload(uuid.NewV7(), uuid.NewV7(), "image/jpeg", 10, o, now)
		if err != nil || u.Origin() != o {
			t.Fatalf("%s: %v %s", o, err, u.Origin())
		}
	}
	if _, err := domain.NewUpload(uuid.NewV7(), uuid.NewV7(), "image/jpeg", 10, "camera", now); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("an unknown origin: %v", err)
	}
	for _, o := range []domain.Origin{domain.OriginFrame, domain.OriginLink, domain.OriginListing} {
		if _, err := domain.NewUpload(uuid.NewV7(), uuid.NewV7(), domain.ContentTypeMP4, 10, o, now); !errors.Is(err, domain.ErrInvalidUpload) {
			t.Fatalf("a video the server made, %s: %v", o, err)
		}
	}
}

func newPhoto(owner uuid.UUID) domain.Upload {
	u, _ := domain.NewUpload(uuid.NewV7(), owner, "image/jpeg", 10, domain.OriginApp, now)
	return u
}

// Extend keeps a waiting photo 30 days from now and never brings its
// expiry in; the photo stays waiting, so a listing still takes it.
func TestExtend(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	u := newPhoto(owner)
	later := now.Add(5 * 24 * time.Hour)
	if err := u.Extend(owner, now); err != nil || !u.ExpiresAt().Equal(now.Add(domain.ExtendFor)) || u.Status() != domain.StatusWaiting {
		t.Fatalf("extend: %v, expires %v, %s", err, u.ExpiresAt(), u.Status())
	}
	if err := u.Extend(owner, later); err != nil || !u.ExpiresAt().Equal(later.Add(domain.ExtendFor)) {
		t.Fatalf("extend later: %v, expires %v", err, u.ExpiresAt())
	}
	if err := u.Extend(owner, now.Add(time.Hour)); err != nil || !u.ExpiresAt().Equal(later.Add(domain.ExtendFor)) {
		t.Fatalf("an earlier extension shortened it: %v, expires %v", err, u.ExpiresAt())
	}
	if u.Expire(now.Add(domain.ExtendFor)) {
		t.Fatal("expired before its extended expiry")
	}
	if changed, err := u.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now.Add(20*24*time.Hour)); err != nil || !changed {
		t.Fatalf("a listing took no extended photo: %v %v", changed, err)
	}
}

func TestExtendRefuses(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	video, _ := domain.NewUpload(uuid.NewV7(), owner, domain.ContentTypeMP4, 10, domain.OriginApp, now)
	attached := newPhoto(owner)
	_, _ = attached.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now)
	for name, c := range map[string]struct {
		u     domain.Upload
		owner uuid.UUID
		at    time.Time
	}{
		"another owner's":         {newPhoto(owner), uuid.NewV7(), now},
		"a video":                 {video, owner, now},
		"a photo past its expiry": {newPhoto(owner), owner, now.Add(domain.ExpireAfter)},
		"an attached photo":       {attached, owner, now},
	} {
		u := c.u
		before := u.ExpiresAt()
		if err := u.Extend(c.owner, c.at); !errors.Is(err, domain.ErrInvalidUpload) || !u.ExpiresAt().Equal(before) {
			t.Errorf("%s: %v, expires %v", name, err, u.ExpiresAt())
		}
	}
}

// Gone is what can no longer be in a draft: taken by a holder, expired, or
// waiting past its expiry. Another owner's upload is not gone; Extend
// refuses it.
func TestGone(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	extended := newPhoto(owner)
	_ = extended.Extend(owner, now)
	attached := newPhoto(owner)
	_, _ = attached.Attach(owner, uuid.NewV7(), domain.ShapePhoto, now)
	for name, c := range map[string]struct {
		u    domain.Upload
		at   time.Time
		gone bool
	}{
		"waiting":                 {newPhoto(owner), now, false},
		"waiting past its expiry": {newPhoto(owner), now.Add(domain.ExpireAfter), true},
		"extended, 29 days on":    {extended, now.Add(29 * 24 * time.Hour), false},
		"attached":                {attached, now, true},
	} {
		u := c.u
		if u.Gone(owner, c.at) != c.gone {
			t.Errorf("%s: gone %v", name, !c.gone)
		}
	}
	if attached.Gone(uuid.NewV7(), now) {
		t.Fatal("another owner's upload is gone")
	}
}

// A stored reason reads as its code, the type named in wrong_type aside.
func TestFailureCodeOf(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]domain.FailureCode{
		"": "", domain.ReasonTooLarge: domain.FailTooLarge, domain.WrongType("image/png"): domain.FailWrongType,
		domain.ReasonUnreadable: domain.FailUnreadable, domain.ReasonTooManyPixels: domain.FailTooManyPixels,
		domain.ReasonUnknownUpload: domain.FailUnknownUpload, domain.ReasonNotAttached: domain.FailNotAttached,
		"something else": domain.FailOther,
	} {
		if got := domain.FailureCodeOf(reason); got != want {
			t.Errorf("%q: %q, want %q", reason, got, want)
		}
	}
}
