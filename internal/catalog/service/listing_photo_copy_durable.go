package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// listingPhotoCopy is the ListingPhotoCopy service's name without the
// prefix.
const listingPhotoCopy = "ListingPhotoCopy"

// PhotoCopyInput names the seller and the listing whose photos are copied.
type PhotoCopyInput struct {
	Seller  uuid.UUID `json:"seller"`
	Listing uuid.UUID `json:"listing"`
}

// PhotoCopyResult names the new uploads, in the listing's photo order, or
// carries the refusal.
type PhotoCopyResult struct {
	Uploads []uuid.UUID   `json:"uploads"`
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r PhotoCopyResult) Refusal() *apperr.Error { return r.Refused }

// photoSource is what the first step of ListingPhotoCopy read: whether the
// seller has the listing, not deleted, and the keys of its processed photos.
type photoSource struct {
	Found  bool               `json:"found"`
	Photos []domain.PhotoKeys `json:"photos"`
}

// ListingPhotoCopy copies the processed photos of a seller's listing into
// new waiting uploads of the seller's, for Đăng lại.
type ListingPhotoCopy struct {
	Prefix   string
	Listings repository.Listings
	Media    mediasvc.Service
	Now      func() time.Time
}

func (s ListingPhotoCopy) ServiceName() string { return s.Prefix + listingPhotoCopy }

// Run is UC-LST-01 A5: it reads the listing in one step, then copies one
// photo per step, one at a time. Each upload ID is minted before its step,
// so a retried step writes the same object and inserts no second upload;
// the photo's bytes never enter the journal. A photo first copied from
// another site keeps that origin, so the new listing marks it copied too.
func (s ListingPhotoCopy) Run(ctx restate.Context, in PhotoCopyInput) (PhotoCopyResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return PhotoCopyResult{}, err
	}
	src, err := restate.Run(ctx, func(rc restate.RunContext) (photoSource, error) {
		l, getErr := s.Listings.Get(rc, in.Listing)
		if errors.Is(getErr, repository.ErrNotFound) {
			return photoSource{}, nil
		}
		if getErr != nil {
			return photoSource{}, fmt.Errorf("get listing: %w", getErr)
		}
		if l.SellerID() != in.Seller || l.Status() == domain.StatusDeleted {
			return photoSource{}, nil
		}
		out := photoSource{Found: true}
		for _, p := range l.Photos() {
			if p.Keys.Photo != "" {
				out.Photos = append(out.Photos, p.Keys)
			}
		}
		return out, nil
	})
	if err != nil {
		return PhotoCopyResult{}, fmt.Errorf("read the listing: %w", err)
	}
	if !src.Found {
		return PhotoCopyResult{Refused: domain.ErrNotFound}, nil
	}
	out := PhotoCopyResult{Uploads: make([]uuid.UUID, 0, len(src.Photos))}
	for i, k := range src.Photos {
		upload := restatex.NewID(ctx, at)
		origin := mediadomain.OriginListing
		if k.Copied {
			origin = mediadomain.OriginLink
		}
		refused, runErr := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
			data, readErr := s.Media.ReadPhoto(rc, k.Photo)
			if readErr != nil {
				return restatex.Refused{}, fmt.Errorf("read photo %d: %w", i, readErr)
			}
			// Processing writes every photo as a JPEG.
			return restatex.AsRefusal(s.Media.SavePhoto(rc, in.Seller, upload, data, "image/jpeg", origin))
		})
		if runErr != nil {
			return PhotoCopyResult{}, fmt.Errorf("copy photo %d: %w", i, runErr)
		}
		if refused.Refusal != nil {
			return PhotoCopyResult{Refused: refused.Refusal}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
		}
		out.Uploads = append(out.Uploads, upload)
	}
	return out, nil
}
