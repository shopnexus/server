package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	authdomain "github.com/shopnexus/server2/internal/auth/domain"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
)

// Open returns the buyer's deal on the listing or starts one. Starting
// needs an account that is not suspended and has a verified phone, on
// someone else's active listing. Two opens at once start one deal.
func (s *Deals) Open(ctx context.Context, buyer, listingID uuid.UUID) (DealView, bool, error) {
	l, err := s.listing(ctx, listingID)
	if err != nil {
		return DealView{}, false, err
	}
	d, err := s.deals.Find(ctx, listingID, buyer)
	if err == nil {
		v, readErr := s.read(ctx, buyer, d, l)
		return v, false, readErr
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return DealView{}, false, fmt.Errorf("find deal: %w", err)
	}
	if err = s.refuseSuspended(ctx, buyer); err != nil {
		return DealView{}, false, err
	}
	ids, err := s.auth.Identities(ctx, buyer)
	if err != nil {
		return DealView{}, false, fmt.Errorf("read identities: %w", err)
	}
	if !slices.ContainsFunc(ids, func(i authsvc.Identity) bool { return i.Kind == authdomain.KindPhone }) {
		return DealView{}, false, domain.ErrPhoneRequired
	}
	if l.SellerID == buyer {
		return DealView{}, false, domain.ErrOwnListing
	}
	if l.Status != catalogdomain.StatusActive {
		return DealView{}, false, domain.ErrListingUnavailable
	}
	d = domain.Open(uuid.NewV7(), listingID, buyer, l.SellerID, s.now())
	created, err := s.deals.Insert(ctx, d)
	if err != nil {
		return DealView{}, false, fmt.Errorf("insert deal: %w", err)
	}
	if !created {
		if d, err = s.deals.Find(ctx, listingID, buyer); err != nil {
			return DealView{}, false, fmt.Errorf("find deal: %w", err)
		}
	}
	v, err := s.read(ctx, buyer, d, l)
	return v, created, err
}
