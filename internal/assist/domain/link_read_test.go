package domain_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

func TestLinkReadLifecycle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	link := domain.Link{Site: domain.SiteChotot, ListID: 119482736, URL: "https://www.chotot.com/119482736.htm"}
	id, me := uuid.NewV7(), uuid.NewV7()
	r := domain.NewLinkRead(id, me, link, now)
	if r.ID() != id || r.AccountID() != me || r.Link() != link || r.Status() != domain.StatusReading || r.WriteID() != id ||
		r.PhotoCount() != 0 || r.Draft() != nil || r.Estimate() != nil || !r.ExpiresAt().Equal(now.Add(24*time.Hour)) {
		t.Fatalf("%+v", r.State())
	}
	if err := r.SetPhotoCount(5); err != nil {
		t.Fatal(err)
	}
	photo := uuid.NewV7()
	if err := r.SetDraft(domain.Draft{Title: "iPhone 11", Photos: []uuid.UUID{photo}}, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Finish(nil); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back domain.LinkRead
	if err = json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Status() != domain.StatusDone || back.PhotoCount() != 5 || back.Draft().Photos[0] != photo || !back.ContactRemoved() ||
		back.Link() != link {
		t.Fatalf("%+v", back.State())
	}
	if err = back.Fail(domain.FailUnavailable); !errors.Is(err, domain.ErrNotReading) {
		t.Fatalf("fail after done: %v", err)
	}
	if !back.Expired(now.Add(24*time.Hour)) || back.Expired(now.Add(time.Hour)) {
		t.Fatal("expiry")
	}
}
