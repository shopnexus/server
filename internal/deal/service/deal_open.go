package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
)

// Open is UC-DEAL-02.
func (s *Deals) Open(ctx context.Context, buyer, listingID uuid.UUID) (DealView, bool, error) {
	l, err := s.listing(ctx, listingID)
	if err != nil {
		return DealView{}, false, err
	}
	// A1. The buyer's deal on the listing opens again as it is.
	d, err := s.deals.Find(ctx, listingID, buyer)
	if err == nil {
		v, readErr := s.read(ctx, buyer, d, l)
		return v, false, readErr
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return DealView{}, false, fmt.Errorf("find deal: %w", err)
	}
	// E1 · BR-MOD-02
	status, err := s.accounts.Status(ctx, buyer)
	if err != nil {
		return DealView{}, false, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return DealView{}, false, r
	}
	// E2 · BR-ACC-03
	hasPhone, err := s.accounts.HasPhone(ctx, buyer)
	if err != nil {
		return DealView{}, false, fmt.Errorf("read the buyer's phone: %w", err)
	}
	if !hasPhone {
		return DealView{}, false, accountdomain.ErrPhoneRequired
	}
	// E3
	if l.SellerID == buyer {
		return DealView{}, false, domain.ErrOwnListing
	}
	// E4 · BR-ACC-01
	blocked, err := s.accounts.Blocked(ctx, buyer, l.SellerID)
	if err != nil {
		return DealView{}, false, fmt.Errorf("check block: %w", err)
	}
	if blocked {
		return DealView{}, false, accountdomain.ErrBlocked
	}
	// E5
	if l.Status != catalogdomain.StatusActive {
		return DealView{}, false, domain.ErrListingUnavailable
	}
	// 1. Open the pair's conversation, or reuse it.
	conversation, opened, err := s.chats.OpenPair(ctx, buyer, l.SellerID)
	if err != nil {
		return DealView{}, false, fmt.Errorf("open the conversation: %w", err)
	}
	d = domain.Open(uuid.NewV7(), conversation, listingID, buyer, l.SellerID, s.now())
	created, err := s.deals.Insert(ctx, d)
	if err != nil {
		// A conversation opened for this deal is dropped again, unless
		// something else uses it by then.
		err = fmt.Errorf("insert deal: %w", err)
		if opened {
			if dropErr := s.chats.DropPair(ctx, conversation); dropErr != nil {
				err = errors.Join(err, fmt.Errorf("drop the conversation: %w", dropErr))
			}
		}
		return DealView{}, false, err
	}
	// NFR-CONC-01: two opens at once start one deal; the one that lost the
	// insert reads the other's.
	if !created {
		if d, err = s.deals.Find(ctx, listingID, buyer); err != nil {
			return DealView{}, false, fmt.Errorf("find deal: %w", err)
		}
	}
	v, err := s.read(ctx, buyer, d, l)
	return v, created, err
}
