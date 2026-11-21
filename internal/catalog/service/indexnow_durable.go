package service

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/provider/indexnow"
)

const (
	// IndexNowName is the IndexNow object's name without the prefix; the
	// listing watch sends to it.
	IndexNowName = "IndexNow"
	// IndexNowQueue is the one key of the IndexNow object.
	IndexNowQueue = "listings"
	// indexNowState is the state entry holding the queued listing IDs.
	indexNowState = "queue"
)

// IndexNow queues the listings whose pages changed and submits their web
// addresses to IndexNow at most once per IndexNowSettings.Every, up to
// indexnow.MaxURLs at a time. It is a Virtual Object with one key,
// IndexNowQueue, so Add and Flush take turns on one queue: Add schedules a
// Flush when the queue was empty, and a Flush that leaves listings behind
// schedules the next.
type IndexNow struct {
	Prefix   string
	Listings repository.Listings
	Settings IndexNowSettings
	Log      *slog.Logger
}

func (s IndexNow) ServiceName() string { return s.Prefix + IndexNowName }

// Add queues listing once.
func (s IndexNow) Add(ctx restate.ObjectContext, listing uuid.UUID) error {
	queue, err := restate.Get[[]uuid.UUID](ctx, indexNowState)
	if err != nil {
		return err
	}
	if slices.Contains(queue, listing) {
		return nil
	}
	if len(queue) == 0 {
		restate.ObjectSend(ctx, s.ServiceName(), restate.Key(ctx), "Flush").Send(restate.Void{}, restate.WithDelay(s.Settings.Every))
	}
	restate.Set(ctx, indexNowState, append(queue, listing))
	return nil
}

// Flush submits the queue's first indexnow.MaxURLs listings that were
// ever on sale, at their current addresses. IndexNow throttling or failing
// is retried a few times with backoff; a refusal or the last failure is
// logged and the listings are dropped, since a page left out is crawled
// in time anyway.
func (s IndexNow) Flush(ctx restate.ObjectContext) error {
	queue, getErr := restate.Get[[]uuid.UUID](ctx, indexNowState)
	if getErr != nil {
		return getErr
	}
	batch := queue[:min(len(queue), indexnow.MaxURLs)]
	if rest := queue[len(batch):]; len(rest) > 0 {
		restate.Set(ctx, indexNowState, rest)
		restate.ObjectSend(ctx, s.ServiceName(), restate.Key(ctx), "Flush").Send(restate.Void{}, restate.WithDelay(s.Settings.Every))
	} else {
		restate.Clear(ctx, indexNowState)
	}
	if len(batch) == 0 || !s.Settings.On() {
		return nil
	}
	site, err := url.Parse(s.Settings.Site)
	if err != nil {
		//nolint:wrapcheck // Restate must receive the terminal error itself
		return restate.TerminalErrorf("indexnow site %q: %v", s.Settings.Site, err)
	}
	sent, err := restate.Run(ctx, func(rc restate.RunContext) (int, error) {
		addrs, stepErr := s.Listings.WebAddresses(rc, batch)
		if stepErr != nil {
			return 0, fmt.Errorf("read web addresses: %w", stepErr)
		}
		if len(addrs) == 0 {
			return 0, nil
		}
		urls := make([]string, len(addrs))
		for i, a := range addrs {
			urls[i] = s.Settings.Site + "/listings/" + domain.Slug(a.Title, a.Code)
		}
		stepErr = s.Settings.Client.Submit(rc, indexnow.Submission{
			Host: site.Host, Key: s.Settings.Key, KeyLocation: s.Settings.Site + "/" + s.Settings.Key + ".txt", URLs: urls,
		})
		if se, ok := errors.AsType[*indexnow.StatusError](stepErr); ok && !se.Retry() {
			return 0, restate.ToTerminalError(stepErr)
		}
		return len(urls), stepErr
	}, restate.WithMaxRetryAttempts(5), restate.WithInitialRetryInterval(10*time.Second), restate.WithRetryIntervalFactor(2))
	if err != nil {
		s.Log.ErrorContext(ctx, "indexnow: the submission failed", "listings", len(batch), "err", err)
		return nil
	}
	s.Log.InfoContext(ctx, "indexnow: submitted", "urls", sent)
	return nil
}
