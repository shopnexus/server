// Package shippingtest stands in for module shipping in other modules'
// tests.
package shippingtest

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/domain"
	"github.com/shopnexus/server/internal/shipping/service"
)

// Linked is module shipping as deal calls it, with every seller linked at
// every carrier and the fake of that carrier answering.
type Linked struct{}

func (Linked) Create(ctx context.Context, _ uuid.UUID, c carrier.Carrier, in carrier.CreateInput) (carrier.Created, error) {
	made, err := carrier.Fake{Carrier: c}.Create(ctx, carrier.Account{}, in)
	if err != nil {
		return carrier.Created{}, fmt.Errorf("fake %s: %w", c, err)
	}
	return made, nil
}

func (Linked) Track(ctx context.Context, _ uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error) {
	p, err := carrier.Fake{Carrier: c}.Lookup(ctx, carrier.Account{}, code)
	if err != nil {
		return carrier.Parcel{}, fmt.Errorf("fake %s: %w", c, err)
	}
	return p, nil
}

func (Linked) Cancel(ctx context.Context, _ uuid.UUID, c carrier.Carrier, code string) error {
	if err := (carrier.Fake{Carrier: c}).Cancel(ctx, carrier.Account{}, code); err != nil {
		return fmt.Errorf("fake %s: %w", c, err)
	}
	return nil
}

func (Linked) SavedPickup(context.Context, uuid.UUID) (service.Pickup, error) {
	return service.Pickup{}, domain.ErrPickupNotConfigured
}
