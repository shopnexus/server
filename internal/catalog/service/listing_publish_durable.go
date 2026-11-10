package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// listingPublish is the ListingPublish service's name without the prefix.
const listingPublish = "ListingPublish"

// ListingPublish waits for a listing's uploads to be processed and puts it
// on sale, or rejects it with the reasons.
type ListingPublish struct {
	Prefix   string
	Listings repository.Listings
	Now      func() time.Time
}

func (s ListingPublish) ServiceName() string { return s.Prefix + listingPublish }

// loadedListing is what the load step found.
type loadedListing struct {
	Listing domain.Listing `json:"listing"`
	Found   bool           `json:"found"`
}

// Run processes every upload the listing holds unprocessed, through
// MediaProcess with the upload ID as idempotency key, so two runs share one
// processing. A listing that changed meanwhile is left as it is: the change
// started a run of its own.
func (s ListingPublish) Run(ctx restate.Context, id uuid.UUID) error {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return err
	}
	ld, err := restate.Run(ctx, func(rc restate.RunContext) (loadedListing, error) {
		l, stepErr := s.Listings.Get(rc, id)
		if errors.Is(stepErr, repository.ErrNotFound) {
			return loadedListing{}, nil
		}
		if stepErr != nil {
			return loadedListing{}, fmt.Errorf("get listing: %w", stepErr)
		}
		return loadedListing{Listing: l, Found: true}, nil
	})
	if err != nil {
		return fmt.Errorf("load listing: %w", err)
	}
	l := ld.Listing
	if !ld.Found || l.Status() != domain.StatusProcessing {
		return nil
	}

	pending := l.PendingUploads()
	results, err := mediasvc.ProcessAll(ctx, s.Prefix, pending)
	if err != nil {
		return fmt.Errorf("process uploads: %w", err)
	}
	keys := make(map[uuid.UUID]domain.PhotoKeys, len(pending))
	reasons := map[uuid.UUID]string{}
	for _, u := range pending {
		if r := results[u]; r.Status == mediadomain.StatusReady {
			keys[u] = domain.PhotoKeys{Photo: r.PhotoKey, Thumb: r.ThumbKey}
		} else {
			reasons[u] = r.Failure
		}
	}

	var evs []events.Event
	if len(reasons) > 0 {
		l.Rejected(reasons, at)
		evs = append(evs, events.Event{ID: restatex.NewID(ctx, at), Kind: events.ListingRejected, SubjectID: id, OccurredAt: at})
	} else {
		ev := events.Event{ID: restatex.NewID(ctx, at), Kind: events.ListingPublished, SubjectID: id, OccurredAt: at}
		// Followers hear of a listing once, when it first goes on sale.
		if l.Published(keys, at) {
			ev.Payload = map[string]any{"first": true}
		}
		evs = append(evs, ev)
	}
	w := restatex.NewID(ctx, at)
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		l := l // Save sets the write ID on its copy; l outside stays as journaled
		return restatex.Ignore(s.Listings.Save(rc, &l, w, evs...), repository.ErrConflict)
	}); err != nil {
		return fmt.Errorf("save listing: %w", err)
	}
	if len(reasons) == 0 {
		startEmbed(ctx, s.Prefix, id)
	}
	return nil
}
