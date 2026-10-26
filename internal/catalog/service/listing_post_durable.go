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
	authdomain "github.com/shopnexus/server2/internal/auth/domain"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// listingPost is the ListingPost service's name without the prefix.
const listingPost = "ListingPost"

type PostInput struct {
	Seller uuid.UUID    `json:"seller"`
	Draft  domain.Draft `json:"draft"`
}

// PostResult names the listing posted, or carries the refusal.
type PostResult struct {
	ListingID uuid.UUID     `json:"listing_id"`
	Refused   *apperr.Error `json:"refused,omitempty"`
}

func (r PostResult) Refusal() *apperr.Error { return r.Refused }

// ListingPost posts a listing: it inserts the row, attaches its uploads and
// starts ListingPublish, each a step, so a retry goes on from the step it
// reached and a lost connection strands nothing.
type ListingPost struct {
	Prefix     string
	Listings   repository.Listings
	Categories repository.Categories
	Regions    repository.Regions
	Accounts   accountsvc.Service
	Auth       authsvc.Service
	Media      mediasvc.Service
	Now        func() time.Time
}

func (s ListingPost) ServiceName() string { return s.Prefix + listingPost }

// Run checks, in order, that the seller is not suspended, has a verified
// phone number and is under the cap, then the draft. The row goes in before
// the uploads are attached: a cap that a racing post filled refuses before
// any upload is taken, and an attach that refuses deletes the row again.
func (s ListingPost) Run(ctx restate.Context, in PostInput) (PostResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return PostResult{}, err
	}
	suspended, err := isSuspended(ctx, s.Accounts, in.Seller)
	if err != nil || suspended {
		return PostResult{Refused: refusedIf(suspended, accountdomain.ErrSuspended)}, err
	}
	hasPhone, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		ids, stepErr := s.Auth.Identities(rc, in.Seller)
		if stepErr != nil {
			return false, fmt.Errorf("read identities: %w", stepErr)
		}
		return slices.ContainsFunc(ids, func(i authsvc.Identity) bool { return i.Kind == authdomain.KindPhone }), nil
	})
	if err != nil {
		return PostResult{}, fmt.Errorf("check the phone: %w", err)
	}
	if !hasPhone {
		return PostResult{Refused: domain.ErrPhoneRequired}, nil
	}
	n, err := restate.Run(ctx, func(rc restate.RunContext) (int, error) { return s.Listings.CountOnSale(rc, in.Seller) })
	if err != nil {
		return PostResult{}, fmt.Errorf("count listings: %w", err)
	}
	if n >= domain.MaxOnSale {
		return PostResult{Refused: domain.ErrCapReached}, nil
	}
	refs, err := lookUpRefs(ctx, s.Categories, s.Regions, in.Draft)
	if err != nil {
		return PostResult{}, err
	}
	id := restatex.NewID(ctx, at)
	l, err := domain.NewListing(id, in.Seller, in.Draft, refs, at)
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
	refused, err := attach(ctx, s.Media, in.Seller, id, in.Draft.Uploads())
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

// isSuspended reports whether the seller's account is suspended.
func isSuspended(ctx restate.Context, accounts accountsvc.Service, seller uuid.UUID) (bool, error) {
	st, err := restate.Run(ctx, func(rc restate.RunContext) (accountdomain.Status, error) {
		return accounts.Status(rc, seller)
	})
	if err != nil {
		return false, fmt.Errorf("read account status: %w", err)
	}
	return st == accountdomain.StatusSuspended, nil
}

// refusedIf is e when refused is true, else no refusal.
func refusedIf(refused bool, e *apperr.Error) *apperr.Error {
	if refused {
		return e
	}
	return nil
}

// attached is what the attach step decided: media's refusal, or none.
type attached struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

// lookUpRefs reads the draft's category and commune.
func lookUpRefs(ctx restate.Context, categories repository.Categories, regions repository.Regions, d domain.Draft) (domain.Refs, error) {
	refs, err := restate.Run(ctx, func(rc restate.RunContext) (domain.Refs, error) {
		leaf, stepErr := categories.IsLeaf(rc, d.CategoryCode)
		if stepErr != nil {
			return domain.Refs{}, fmt.Errorf("read category: %w", stepErr)
		}
		exists, stepErr := regions.CommuneExists(rc, d.CommuneCode)
		if stepErr != nil {
			return domain.Refs{}, fmt.Errorf("read commune: %w", stepErr)
		}
		return domain.Refs{CategoryIsLeaf: leaf, CommuneExists: exists}, nil
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
	out, err := restate.Run(ctx, func(rc restate.RunContext) (attached, error) {
		stepErr := media.Attach(rc, seller, holder, ids)
		if e, ok := errors.AsType[*apperr.Error](stepErr); ok && e.Kind != apperr.KindInternal {
			return attached{Refused: e}, nil
		}
		if stepErr != nil {
			return attached{}, fmt.Errorf("attach: %w", stepErr)
		}
		return attached{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("attach uploads: %w", err)
	}
	return out.Refused, nil
}

// startPublish sends ListingPublish for the listing as written under
// writeID. The send is journaled, so it is never lost, and the idempotency
// key names the write, so the next write starts a run of its own.
func startPublish(ctx restate.Context, prefix string, id, writeID uuid.UUID) {
	restate.ServiceSend(ctx, prefix+listingPublish, "Run").
		Send(id, restate.WithIdempotencyKey(id.String()+"/"+writeID.String()))
}
