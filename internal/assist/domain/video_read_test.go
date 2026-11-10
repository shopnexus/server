package domain_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

var start = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

func newRead() domain.VideoRead {
	return domain.NewVideoRead(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), start)
}

func TestNewVideoRead(t *testing.T) {
	t.Parallel()
	id, account, video := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	r := domain.NewVideoRead(id, account, video, start)
	if r.ID() != id || r.AccountID() != account || r.VideoID() != video || r.Status() != domain.StatusReading ||
		r.Failure() != "" || r.Frames() != nil || r.Draft() != nil || r.Estimate() != nil ||
		!r.CreatedAt().Equal(start) || !r.ExpiresAt().Equal(start.Add(24*time.Hour)) || r.WriteID() != id {
		t.Fatalf("%+v", r.State())
	}
}

func TestVideoReadSteps(t *testing.T) {
	t.Parallel()
	r := newRead()
	frames := []domain.Frame{{UploadID: uuid.NewV7(), AtSeconds: 0.5}, {UploadID: uuid.NewV7(), AtSeconds: 3}}
	if err := r.SetFrames(frames); err != nil {
		t.Fatal(err)
	}
	frames[0].AtSeconds = 99 // the read keeps its own copy
	if got := r.Frames(); len(got) != 2 || got[0].AtSeconds != 0.5 {
		t.Fatalf("frames %+v", got)
	}
	price := int64(4_500_000)
	d := domain.Draft{
		Title: "iPhone 11 64GB Trắng", CategoryCode: "phones.iphone", Condition: catalogdomain.ConditionUsed,
		Specs: []catalogdomain.Spec{{Label: "Dung lượng", Value: "64 GB"}}, Description: "Máy dùng 3 năm.",
		Price: &price, AllowOffers: true, Photos: []uuid.UUID{frames[1].UploadID},
	}
	if err := r.SetDraft(d); err != nil {
		t.Fatal(err)
	}
	if got := r.Draft(); got == nil || got.Title != d.Title || *got.Price != price || got.Photos[0] != frames[1].UploadID {
		t.Fatalf("draft %+v", got)
	}
	e := &domain.Estimate{Low: 4_400_000, Median: 4_800_000, High: 5_100_000, Source: domain.SourceMarket, SampleSize: 7}
	if err := r.Finish(e); err != nil {
		t.Fatal(err)
	}
	if r.Status() != domain.StatusDone || r.Estimate() == nil || r.Estimate().Median != 4_800_000 {
		t.Fatalf("finished %+v", r.State())
	}
	for name, err := range map[string]error{
		"frames": r.SetFrames(frames), "draft": r.SetDraft(d), "finish": r.Finish(nil), "fail": r.Fail(domain.FailUnavailable),
	} {
		if !errors.Is(err, domain.ErrNotReading) {
			t.Errorf("%s after done: %v", name, err)
		}
	}
}

func TestVideoReadFinishesWithoutEstimate(t *testing.T) {
	t.Parallel()
	r := newRead()
	if err := r.Finish(nil); err != nil || r.Status() != domain.StatusDone || r.Estimate() != nil {
		t.Fatalf("%+v %v", r.State(), err)
	}
}

func TestVideoReadFails(t *testing.T) {
	t.Parallel()
	r := newRead()
	if err := r.Fail(domain.FailTooShort); err != nil {
		t.Fatal(err)
	}
	if r.Status() != domain.StatusFailed || r.Failure() != "video_too_short" {
		t.Fatalf("%+v", r.State())
	}
	if err := r.Fail(domain.FailUnavailable); !errors.Is(err, domain.ErrNotReading) {
		t.Fatalf("second fail: %v", err)
	}
}

func TestVideoReadClock(t *testing.T) {
	t.Parallel()
	r := newRead()
	cases := []struct {
		at                time.Duration
		expired, deadline bool
	}{
		{0, false, false},
		{2*time.Minute - time.Nanosecond, false, false},
		{2 * time.Minute, false, true},
		{24*time.Hour - time.Nanosecond, false, true},
		{24 * time.Hour, true, true},
	}
	for _, c := range cases {
		now := start.Add(c.at)
		if r.Expired(now) != c.expired || r.PastDeadline(now) != c.deadline {
			t.Errorf("at +%v: expired %v deadline %v", c.at, r.Expired(now), r.PastDeadline(now))
		}
	}
}

// A Restate step returns a read to its journal; the replay must rebuild
// the same read.
func TestVideoReadJournal(t *testing.T) {
	t.Parallel()
	r := newRead()
	if err := r.SetFrames([]domain.Frame{{UploadID: uuid.NewV7(), AtSeconds: 1.5}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetDraft(domain.Draft{
		Title: "Xe đạp", CategoryCode: "vehicles.bicycles", Condition: catalogdomain.ConditionLikeNew,
		Specs: []catalogdomain.Spec{}, Photos: []uuid.UUID{uuid.NewV7()},
	}); err != nil {
		t.Fatal(err)
	}
	r.Saved(uuid.NewV7())
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back domain.VideoRead
	if err = json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(back)
	if err != nil || !bytes.Equal(b, again) || back.WriteID() != r.WriteID() {
		t.Fatalf("%s\n%s\n%v", b, again, err)
	}
	if !bytes.Contains(b, []byte(`"price":null`)) {
		t.Fatalf("an unsaid price is null in the journal: %s", b)
	}
}
