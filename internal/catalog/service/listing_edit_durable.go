package service

import (
	"fmt"
	"slices"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// listingEdit is the ListingEdit service's name without the prefix.
const listingEdit = "ListingEdit"

type EditInput struct {
	Seller uuid.UUID `json:"seller"`
	ID     uuid.UUID `json:"id"`
	// The wire key stays "draft" so old and new pods agree during a deploy.
	Listing domain.ListingInput `json:"draft"`
}

// EditResult carries the refusal of an edit, if any.
type EditResult struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r EditResult) Refusal() *apperr.Error { return r.Refused }

// tidyAfter is how long uploads an edit attached and then lost its save
// with wait for the seller's retry, as long as an upload waits for an attach.
const tidyAfter = 24 * time.Hour

// TidyInput names the uploads an edit of listing ID attached before it
// lost its save.
type TidyInput struct {
	ID      uuid.UUID   `json:"id"`
	Uploads []uuid.UUID `json:"uploads"`
}

// ListingEdit replaces a listing's content, attaches the uploads the edit
// adds, has media release the ones it drops and, when the listing is left
// in processing, starts ListingPublish.
type ListingEdit struct{ flowDeps }

func (s ListingEdit) ServiceName() string { return s.Prefix + listingEdit }

// Run is UC-LST-02, step 3: it edits the seller's listing. Uploads attached
// by an edit that then lost a race stay with the listing, so the client's
// next try attaches them again without a refusal; Tidy releases those the
// listing still lacks tidyAfter later.
func (s ListingEdit) Run(ctx restate.Context, in EditInput) (EditResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return EditResult{}, err
	}
	// E4 · BR-LST-02
	status, err := restate.Run(ctx, func(rc restate.RunContext) (accountdomain.Status, error) {
		return s.Accounts.Status(rc, in.Seller)
	})
	if err != nil {
		return EditResult{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return EditResult{Refused: r}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	ld, err := loadListing(ctx, s.Listings, in.ID)
	if err != nil {
		return EditResult{}, fmt.Errorf("load listing: %w", err)
	}
	l := ld.Listing
	// E2
	if !ld.Found || l.SellerID() != in.Seller {
		return EditResult{Refused: domain.ErrNotFound}, nil
	}
	refs, err := lookUpRefs(ctx, s.Categories, s.Regions, in.Listing)
	if err != nil {
		return EditResult{}, err
	}
	before := l.Uploads()
	// E1, E3 · BR-LST-03: a state that takes no edit, or a field that breaks a rule.
	if err = l.Edit(in.Listing, refs, at); err != nil {
		return EditResult{Refused: apperr.From(err)}, nil
	}
	added := slices.DeleteFunc(slices.Clone(in.Listing.Photos), func(u uuid.UUID) bool { return slices.Contains(before, u) })
	// E7: an added upload that is not the seller's, or not waiting.
	if len(added) > 0 {
		refused, attachErr := attach(ctx, s.Media, in.Seller, in.ID, added, restatex.NewID(ctx, at))
		if attachErr != nil || refused != nil {
			return EditResult{Refused: refused}, attachErr
		}
	}
	// 3. Save the content.
	w, evID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		l := l // Save sets the write ID on its copy; l outside stays as journaled
		stepErr := s.Listings.Save(rc, &l, w, nil, events.Event{ID: evID, Kind: events.ListingUpdated, SubjectID: in.ID, OccurredAt: at})
		return restatex.Outcome(stepErr, repository.ErrConflict)
	})
	if err != nil {
		return EditResult{}, fmt.Errorf("save listing: %w", err)
	}
	// E8 · NFR-CONC-01
	if lost {
		if len(added) > 0 {
			restate.ServiceSend(ctx, s.ServiceName(), "Tidy").Send(TidyInput{ID: in.ID, Uploads: added}, restate.WithDelay(tidyAfter))
		}
		return EditResult{Refused: apperr.ErrTryAgain}, nil
	}
	mediasvc.Release(ctx, s.Prefix, in.ID, slices.DeleteFunc(before, func(u uuid.UUID) bool { return slices.Contains(in.Listing.Photos, u) }))
	if l.Status() == domain.StatusProcessing {
		startPublish(ctx, s.Prefix, in.ID, w)
	} else {
		startEmbed(ctx, s.Prefix, in.ID)
	}
	return EditResult{}, nil
}

// Tidy releases the uploads of in that listing in.ID does not show: an
// edit attached them and lost its save, and no retry saved them since.
func (s ListingEdit) Tidy(ctx restate.Context, in TidyInput) error {
	ld, err := loadListing(ctx, s.Listings, in.ID)
	if err != nil {
		return fmt.Errorf("load listing: %w", err)
	}
	shown := ld.Listing.Uploads()
	mediasvc.Release(ctx, s.Prefix, in.ID, slices.DeleteFunc(slices.Clone(in.Uploads), func(u uuid.UUID) bool { return slices.Contains(shown, u) }))
	return nil
}
