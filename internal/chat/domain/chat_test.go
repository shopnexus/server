package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.NewV7()
	}
	return out
}

func newMessage(body string, photos []uuid.UUID) (domain.Message, error) {
	return domain.NewMessage(uuid.NewV7(), uuid.NewV7(), uuid.NewV7(), body, photos, nil, now)
}

func TestNewMessage(t *testing.T) {
	t.Parallel()
	m, err := newMessage("  Còn không bạn?  ", nil)
	if err != nil || m.Body() != "Còn không bạn?" || m.Status() != domain.StatusSent {
		t.Fatalf("text: %+v %v", m.State(), err)
	}
	m, err = newMessage("", ids(2))
	if err != nil || m.Status() != domain.StatusProcessing || len(m.Photos()) != 2 {
		t.Fatalf("photos: %+v %v", m.State(), err)
	}
	dup := uuid.NewV7()
	for name, c := range map[string]struct {
		body   string
		photos []uuid.UUID
		field  string
	}{
		"empty":           {"   ", nil, "text"},
		"too long":        {strings.Repeat("a", 1001), nil, "text"},
		"five photos":     {"", ids(5), "photos"},
		"duplicate photo": {"", []uuid.UUID{dup, dup}, "photos"},
	} {
		_, err := newMessage(c.body, c.photos)
		if e, ok := errors.AsType[*apperr.Error](err); !ok || !errors.Is(err, domain.ErrInvalidMessage) || e.Fields[c.field] == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMessageVisibility(t *testing.T) {
	t.Parallel()
	sender, other := uuid.NewV7(), uuid.NewV7()
	photo := uuid.NewV7()
	m, err := domain.NewMessage(uuid.NewV7(), uuid.NewV7(), sender, "", []uuid.UUID{photo}, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if !m.VisibleTo(sender) || m.VisibleTo(other) {
		t.Fatal("processing")
	}
	sent := m
	sent.Published(map[uuid.UUID]domain.Photo{photo: {UploadID: photo, PhotoKey: "media/p.jpg", ThumbKey: "media/p_thumb.jpg"}})
	if sent.Status() != domain.StatusSent || !sent.VisibleTo(other) || sent.Photos()[0].PhotoKey != "media/p.jpg" {
		t.Fatalf("sent: %+v", sent.State())
	}
	failed := m
	failed.Failed("not an image of its type")
	if failed.Status() != domain.StatusFailed || failed.Failure() == "" || failed.VisibleTo(other) || !failed.VisibleTo(sender) {
		t.Fatalf("failed: %+v", failed.State())
	}
}

func TestConversationPair(t *testing.T) {
	t.Parallel()
	a, b := uuid.NewV7(), uuid.NewV7()
	one, two := domain.NewConversation(uuid.NewV7(), a, b, now), domain.NewConversation(uuid.NewV7(), b, a, now)
	if one.AccountA != two.AccountA || one.AccountB != two.AccountB {
		t.Fatalf("%+v %+v", one, two)
	}
	if !one.Has(a) || !one.Has(b) || one.Has(uuid.NewV7()) || one.Other(a) != b || one.Other(b) != a {
		t.Fatalf("%+v", one)
	}
}
