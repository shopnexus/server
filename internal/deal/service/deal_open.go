package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/repository"
)

// Open returns the buyer's deal on the listing or starts one. Starting
// needs an account that is not suspended and has a phone, on someone else's active
// listing, with no block between the two; it opens the pair's conversation
// or reuses it. Two opens at once start one deal in one conversation. A
// conversation opened for a deal that could not be stored is dropped
// again, unless something else uses it by then.
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
	status, err := s.accounts.Status(ctx, buyer)
	if err != nil {
		return DealView{}, false, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return DealView{}, false, r
	}
	hasPhone, err := s.accounts.HasPhone(ctx, buyer)
	if err != nil {
		return DealView{}, false, fmt.Errorf("read the buyer's phone: %w", err)
	}
	if !hasPhone {
		return DealView{}, false, accountdomain.ErrPhoneRequired
	}
	if l.SellerID == buyer {
		return DealView{}, false, domain.ErrOwnListing
	}
	blocked, err := s.accounts.Blocked(ctx, buyer, l.SellerID)
	if err != nil {
		return DealView{}, false, fmt.Errorf("check block: %w", err)
	}
	if blocked {
		return DealView{}, false, accountdomain.ErrBlocked
	}
	if l.Status != catalogdomain.StatusActive {
		return DealView{}, false, domain.ErrListingUnavailable
	}
	conversation, opened, err := s.chats.OpenPair(ctx, buyer, l.SellerID)
	if err != nil {
		return DealView{}, false, fmt.Errorf("open the conversation: %w", err)
	}
	d = domain.Open(uuid.NewV7(), conversation, listingID, buyer, l.SellerID, s.now())
	created, err := s.deals.Insert(ctx, d)
	if err != nil {
		err = fmt.Errorf("insert deal: %w", err)
		if opened {
			if dropErr := s.chats.DropPair(ctx, conversation); dropErr != nil {
				err = errors.Join(err, fmt.Errorf("drop the conversation: %w", dropErr))
			}
		}
		return DealView{}, false, err
	}
	if !created {
		if d, err = s.deals.Find(ctx, listingID, buyer); err != nil {
			return DealView{}, false, fmt.Errorf("find deal: %w", err)
		}
	}
	v, err := s.read(ctx, buyer, d, l)
	return v, created, err
}
