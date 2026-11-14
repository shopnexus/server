package service

import (
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// listingPost is the ListingPost service's name without the prefix.
const listingPost = "ListingPost"

type PostInput struct {
	Seller uuid.UUID `json:"seller"`
	// The wire key stays "draft" so old and new pods agree during a deploy.
	Listing domain.ListingInput `json:"draft"`
}

// PostResult names the listing posted, or carries the refusal.
type PostResult struct {
	ListingID uuid.UUID     `json:"listing_id"`
	Refused   *apperr.Error `json:"refused,omitempty"`
}

func (r PostResult) Refusal() *apperr.Error { return r.Refused }

// flowDeps is what ListingPost and ListingEdit are built from.
type flowDeps struct {
	Prefix     string
	Listings   repository.Listings
	Categories repository.Categories
	Regions    repository.Regions
	Accounts   accountsvc.Service
	Media      mediasvc.Service
	Now        func() time.Time
}

// ListingPost posts a listing: it inserts the row, attaches its uploads and
// starts ListingPublish, each a step, so a retry goes on from the step it
// reached and a lost connection strands nothing.
type ListingPost struct{ flowDeps }

func (s ListingPost) ServiceName() string { return s.Prefix + listingPost }

// Run checks, in order, that the seller is not suspended, has a phone and
// is under the cap, then the listing's fields. The row goes in before
// the uploads are attached: a cap that a racing post filled refuses before
// any upload is taken, and an attach that refuses deletes the row again.
func (s ListingPost) Run(ctx restate.Context, in PostInput) (PostResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return PostResult{}, err
	}
	status, err := restate.Run(ctx, func(rc restate.RunContext) (accountdomain.Status, error) {
		return s.Accounts.Status(rc, in.Seller)
	})
	if err != nil {
		return PostResult{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return PostResult{Refused: r}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	hasPhone, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		return s.Accounts.HasPhone(rc, in.Seller)
	})
	if err != nil {
		return PostResult{}, fmt.Errorf("read the seller's phone: %w", err)
	}
	if !hasPhone {
		return PostResult{Refused: accountdomain.ErrPhoneRequired}, nil
	}
	n, err := restate.Run(ctx, func(rc restate.RunContext) (int, error) {
		counts, stepErr := s.Listings.CountByStatus(rc, in.Seller)
		if stepErr != nil {
			return 0, fmt.Errorf("count by status: %w", stepErr)
		}
		return domain.CountIn(counts, domain.OnSale), nil
	})
	if err != nil {
		return PostResult{}, fmt.Errorf("count listings: %w", err)
	}
	if n >= domain.MaxOnSale {
		return PostResult{Refused: domain.ErrCapReached}, nil
	}
	refs, err := lookUpRefs(ctx, s.Categories, s.Regions, in.Listing)
	if err != nil {
		return PostResult{}, err
	}
	id := restatex.NewID(ctx, at)
	l, err := domain.NewListing(id, in.Seller, in.Listing, refs, at)
	if err != nil {
		return PostResult{Refused: apperr.From(err)}, nil
	}

	capped, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		return restatex.Outcome(s.Listings.Insert(rc, l), domain.ErrCapReached)
	})
	if err != nil {
		return PostResult{}, fmt.Errorf("insert listing: %w", err)
	}
	if capped {
		return PostResult{Refused: domain.ErrCapReached}, nil
	}
	refused, err := attach(ctx, s.Media, in.Seller, id, in.Listing.Photos)
	if err != nil {
		return PostResult{}, err
	}
	if refused != nil {
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Listings.Discard(rc, id) }); err != nil {
			return PostResult{}, fmt.Errorf("undo listing: %w", err)
		}
		return PostResult{Refused: refused}, nil
	}
	startPublish(ctx, s.Prefix, id, l.WriteID())
	return PostResult{ListingID: id}, nil
}

// lookUpRefs reads the listing's category and commune.
func lookUpRefs(ctx restate.Context, categories repository.Categories, regions repository.Regions, d domain.ListingInput) (domain.Refs, error) {
	refs, err := restate.Run(ctx, func(rc restate.RunContext) (domain.Refs, error) {
		leaf, stepErr := categories.IsLeaf(rc, d.CategoryCode)
		if stepErr != nil {
			return domain.Refs{}, fmt.Errorf("read category: %w", stepErr)
		}
		places, stepErr := regions.Places(rc, []string{d.CommuneCode})
		if stepErr != nil {
			return domain.Refs{}, fmt.Errorf("read commune: %w", stepErr)
		}
		refs := domain.Refs{CategoryIsLeaf: leaf}
		if len(places) == 1 {
			refs.CommuneExists = true
			refs.CommunePoint = domain.Point{Lat: places[0].Lat, Lng: places[0].Lng}
		}
		return refs, nil
	})
	if err != nil {
		return domain.Refs{}, fmt.Errorf("look up category and commune: %w", err)
	}
	return refs, nil
}

// attach gives the uploads to the listing holder and returns media's
// refusal, invalid_upload or try_again, as data. A retried attach passes,
// since holder already holds them.
func attach(ctx restate.Context, media mediasvc.Service, seller, holder uuid.UUID, ids []uuid.UUID) (*apperr.Error, error) {
	out, err := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
		return restatex.AsRefusal(media.Attach(rc, seller, holder, ids))
	})
	if err != nil {
		return nil, fmt.Errorf("attach uploads: %w", err)
	}
	return out.Refusal, nil
}

// startPublish sends ListingPublish for the listing as written under
// writeID. The send is journaled, so it is never lost, and the idempotency
// key names the write, so the next write starts a run of its own.
func startPublish(ctx restate.Context, prefix string, id, writeID uuid.UUID) {
	restate.ServiceSend(ctx, prefix+listingPublish, "Run").
		Send(id, restate.WithIdempotencyKey(id.String()+"/"+writeID.String()))
}

// startEmbed asks ListingEmbed to bring the listing's vectors in line. A send
// to an object queues behind the listing's run in progress.
func startEmbed(ctx restate.Context, prefix string, id uuid.UUID) {
	restate.ObjectSend(ctx, prefix+ListingEmbedName, id.String(), "Refresh").Send(restate.Void{})
}
