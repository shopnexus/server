package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/service"
)

// A watch sends the read, the text as it grows, then the read once done.
//
// UC-LST-01.
func TestWatchReadSendsTheText(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(e *env) { e.model.gap = 300 * time.Millisecond })
	seller := accounttest.WithPhone(t, e.db)
	started := e.startPhoto(t, seller, e.photo(t, seller))
	var events []service.ReadEvent
	err := e.assist.WatchRead(context.Background(), seller, started.ID, func(ev service.ReadEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	first, last := events[0], events[len(events)-1]
	if first.Read == nil || first.Read.Status != domain.StatusReading {
		t.Fatalf("first %+v", first)
	}
	if last.Read == nil || last.Read.Status != domain.StatusDone || last.Read.Draft == nil || last.Read.Draft.Title != "iPhone 11 Trắng" {
		t.Fatalf("last %+v", last)
	}
	var texts []domain.Text
	for _, ev := range events[1 : len(events)-1] {
		if ev.Text != nil {
			texts = append(texts, *ev.Text)
		}
	}
	// The first text may carry only the start of the title.
	title := "iPhone 11 Trắng"
	if len(texts) < 2 || !strings.HasPrefix(title, texts[0].Title) ||
		texts[len(texts)-1].Description != "iPhone 11 màu trắng, mặt lưng kính, cụm 2 camera sau." {
		t.Fatalf("texts %+v", texts)
	}
}

// A read that has ended gets one event; another account's read is not
// found and gets none.
func TestWatchReadOfAnEndedRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, other := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	v := e.end(t, seller, e.startPhoto(t, seller, e.photo(t, seller)).ID)
	var n int
	count := func(service.ReadEvent) error { n++; return nil }
	if err := e.assist.WatchRead(context.Background(), seller, v.ID, count); err != nil || n != 1 {
		t.Fatalf("%d events, %v", n, err)
	}
	if err := e.assist.WatchRead(context.Background(), other, v.ID, count); !errors.Is(err, domain.ErrNotFound) || n != 1 {
		t.Fatalf("%d events, %v", n, err)
	}
}
