package service

import (
	"context"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
)

func (d *Discovery) Hide(ctx context.Context, account, listing uuid.UUID) error {
	l, ok, err := d.catalog.Summary(ctx, listing)
	if err != nil {
		return fmt.Errorf("read listing: %w", err)
	}
	switch {
	case !ok || l.Status == catalogdomain.StatusDeleted:
		return catalogdomain.ErrNotFound
	case l.SellerID == account:
		return domain.ErrOwnListing
	default:
	}
	if err = d.hidden.Hide(ctx, account, listing); err != nil {
		return fmt.Errorf("hide: %w", err)
	}
	return nil
}

func (d *Discovery) Unhide(ctx context.Context, account, listing uuid.UUID) error {
	if err := d.hidden.Unhide(ctx, account, listing); err != nil {
		return fmt.Errorf("unhide: %w", err)
	}
	return nil
}

// exclusions are the listings account hid and the accounts on either side
// of its blocks, which its searches and its watches' sweeps leave out.
func (d *Discovery) exclusions(ctx context.Context, account uuid.UUID) (listings, sellers []uuid.UUID, err error) {
	if listings, err = d.hidden.List(ctx, account); err != nil {
		return nil, nil, fmt.Errorf("read hides: %w", err)
	}
	if sellers, err = d.accounts.BlockedWith(ctx, account); err != nil {
		return nil, nil, fmt.Errorf("read blocks: %w", err)
	}
	return listings, sellers, nil
}
