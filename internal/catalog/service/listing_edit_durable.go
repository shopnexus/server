package service

import (
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// listingEdit is the ListingEdit service's name without the prefix.
const listingEdit = "ListingEdit"

type EditInput struct {
	Seller uuid.UUID    `json:"seller"`
	ID     uuid.UUID    `json:"id"`
	Draft  domain.Draft `json:"draft"`
}

// EditResult carries the refusal of an edit, if any.
type EditResult struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r EditResult) Refusal() *apperr.Error { return r.Refused }

// ListingEdit replaces a listing's content, attaches the uploads the edit
// adds and, when the listing is left in processing, starts ListingPublish.
type ListingEdit struct {
	Prefix     string
	Listings   repository.Listings
	Categories repository.Categories
	Regions    repository.Regions
	Accounts   accountsvc.Service
	Media      mediasvc.Service
	Now        func() time.Time
}

func (s ListingEdit) ServiceName() string { return s.Prefix + listingEdit }

// Run edits the seller's listing. Uploads attached by an edit that then
// lost a race stay with the listing, so the client's next try attaches them
// again without a refusal.
func (s ListingEdit) Run(ctx restate.Context, in EditInput) (EditResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return EditResult{}, err
	}
	suspended, err := isSuspended(ctx, s.Accounts, in.Seller)
	if err != nil || suspended {
		return EditResult{Refused: refusedIf(suspended, accountdomain.ErrSuspended)}, err
	}
	ld, err := restate.Run(ctx, func(rc restate.RunContext) (loadedListing, error) {
		l, stepErr := s.Listings.Get(rc, in.ID)
		if errors.Is(stepErr, repository.ErrNotFound) {
			return loadedListing{}, nil
		}
		if stepErr != nil {
			return loadedListing{}, fmt.Errorf("get listing: %w", stepErr)
		}
		return loadedListing{Listing: l, Found: true}, nil
	})
	if err != nil {
		return EditResult{}, fmt.Errorf("load listing: %w", err)
	}
	l := ld.Listing
	if !ld.Found || l.SellerID() != in.Seller {
		return EditResult{Refused: domain.ErrNotFound}, nil
	}
	refs, err := lookUpRefs(ctx, s.Categories, s.Regions, in.Draft)
	if err != nil {
		return EditResult{}, err
	}
	before := l.Uploads()
	if err = l.Edit(in.Draft, refs, at); err != nil {
		return EditResult{Refused: apperr.From(err)}, nil
	}
	if added := slices.DeleteFunc(slices.Clone(in.Draft.Photos), func(u uuid.UUID) bool { return slices.Contains(before, u) }); len(added) > 0 {
		var refused *apperr.Error
		refused, err = attach(ctx, s.Media, in.Seller, in.ID, added)
		if err != nil || refused != nil {
			return EditResult{Refused: refused}, err
		}
	}
	w, evID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		l := l // Save sets the write ID on its copy; l outside stays as journaled
		stepErr := s.Listings.Save(rc, &l, w, events.Event{ID: evID, Kind: events.ListingUpdated, SubjectID: in.ID, OccurredAt: at})
		return restatex.Outcome(stepErr, repository.ErrConflict)
	})
	if err != nil {
		return EditResult{}, fmt.Errorf("save listing: %w", err)
	}
	if lost {
		return EditResult{Refused: apperr.ErrTryAgain}, nil
	}
	if l.Status() == domain.StatusProcessing {
		startPublish(ctx, s.Prefix, in.ID, w)
	}
	return EditResult{}, nil
}
