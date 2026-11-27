package domain_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
)

var at = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

func TestSourcesAndCaps(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		source domain.Source
		cap    int
	}{{domain.SourceVideo, 30}, {domain.SourcePhoto, 60}} {
		if got := c.source.Cap(); got != c.cap {
			t.Errorf("%s cap %d, want %d", c.source, got, c.cap)
		}
	}
}

func TestNewReadsCarryTheirSource(t *testing.T) {
	t.Parallel()
	id, me, media := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	v, p := domain.NewVideoRead(id, me, media, at), domain.NewPhotoRead(id, me, media, at)
	if v.Source() != domain.SourceVideo || v.Media() != media {
		t.Fatalf("video %+v", v.State())
	}
	if p.Source() != domain.SourcePhoto || p.Media() != media {
		t.Fatalf("photo %+v", p.State())
	}
	for _, r := range []domain.ListingRead{v, p} {
		if r.Status() != domain.StatusReading || r.WriteID() != id || !r.ExpiresAt().Equal(at.Add(24*time.Hour)) ||
			r.Photos() != nil || r.Draft() != nil {
			t.Fatalf("%s %+v", r.Source(), r.State())
		}
	}
}

// Each source ends only with the failures it can have.
func TestFailuresBySource(t *testing.T) {
	t.Parallel()
	me := uuid.NewV7()
	for _, c := range []struct {
		read domain.ListingRead
		ok   []domain.Failure
		bad  []domain.Failure
	}{
		{
			domain.NewVideoRead(uuid.NewV7(), me, uuid.NewV7(), at),
			[]domain.Failure{domain.FailUnreadable, domain.FailTooShort, domain.FailTooLong, domain.FailNoItem, domain.FailUnavailable},
			nil,
		},
		{
			domain.NewPhotoRead(uuid.NewV7(), me, uuid.NewV7(), at),
			[]domain.Failure{domain.FailUnreadable, domain.FailNoItem, domain.FailUnavailable},
			[]domain.Failure{domain.FailTooShort, domain.FailTooLong},
		},
	} {
		for _, f := range c.ok {
			r := c.read
			if err := r.Fail(f, "  Lý do.  "); err != nil || r.Status() != domain.StatusFailed || r.Failure() != f ||
				r.FailureReason() != "Lý do." {
				t.Errorf("%s fails with %s: %v", r.Source(), f, err)
			}
			if err := r.Fail(f, ""); !errors.Is(err, domain.ErrNotReading) {
				t.Errorf("%s failed twice: %v", r.Source(), err)
			}
		}
		for _, f := range c.bad {
			r := c.read
			if err := r.Fail(f, ""); !errors.Is(err, domain.ErrWrongFailure) || r.Status() != domain.StatusReading {
				t.Errorf("%s took %s: %v", r.Source(), f, err)
			}
		}
	}
}

// A read survives the journal: a step returns it as JSON and the next
// reads it back whole.
func TestReadRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	r := domain.NewVideoRead(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), at)
	zero, four := 0.0, 4.0
	photos := []domain.Photo{{UploadID: uuid.NewV7(), AtSeconds: &zero}, {UploadID: uuid.NewV7(), AtSeconds: &four}}
	price := int64(1_200_000)
	if err := r.SetPhotos(photos); err != nil {
		t.Fatal(err)
	}
	if err := r.SetDraft(domain.Draft{Title: "Xe đạp", Price: &price, Photos: []uuid.UUID{photos[1].UploadID}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Finish(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back domain.ListingRead
	if err = json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	got := back.Photos()
	if back.Source() != domain.SourceVideo || back.Status() != domain.StatusDone || len(got) != 2 ||
		got[0].AtSeconds == nil || *got[0].AtSeconds != 0 || back.Draft().Title != "Xe đạp" {
		t.Fatalf("%s", b)
	}
}
