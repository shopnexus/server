package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/provider/carrier"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
)

// Shipping is what deal asks of module shipping: shipments on the seller's
// linked account at a carrier. Its refusals are shipping's errors, which
// deal names domain.ErrCarrierUnavailable, ErrCarrierNotLinked and
// ErrCarrierAddressUnknown; carrier.ErrNotFound and
// carrier.ErrNotCancellable come back as they are.
type Shipping interface {
	SavedPickup(ctx context.Context, seller uuid.UUID) (shippingsvc.Pickup, error)
	Create(ctx context.Context, seller uuid.UUID, c carrier.Carrier, in carrier.CreateInput) (carrier.Created, error)
	Lookup(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error)
	// Track is Lookup for the order's own shipment, which also keeps the
	// carrier's events for the parcel's timeline.
	Track(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) (carrier.Parcel, error)
	Cancel(ctx context.Context, seller uuid.UUID, c carrier.Carrier, code string) error
}

// shippingError turns shipping's errors into deal's answers: notFound for
// a code the carrier does not know, and a carrier refusal as its bare
// sentinel. Anything else is wrapped as is.
func shippingError(err, notFound error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, carrier.ErrNotFound):
		return notFound
	case errors.Is(err, carrier.ErrUnavailable), errors.Is(err, domain.ErrCarrierUnavailable):
		return domain.ErrCarrierUnavailable
	case errors.Is(err, domain.ErrCarrierNotLinked):
		return domain.ErrCarrierNotLinked
	case errors.Is(err, domain.ErrCarrierAddressUnknown):
		return domain.ErrCarrierAddressUnknown
	}
	return fmt.Errorf("carrier: %w", err)
}

// manualShipment reads a manual shipment's carrier and code with the
// caller's account at the carrier. A code the carrier does not know is
// domain.ErrShipmentUnknown (E1) and a stack without a carrier
// domain.ErrCarrierUnavailable (E2), for any order. A carrier the caller
// has not linked (E3) comes after the order check, since a stranger or the
// buyer links nothing and gets their order answer first.
func (s *Deals) manualShipment(ctx context.Context, caller, order uuid.UUID, carrierCode, trackingCode string) (domain.Shipment, error) {
	c, code, err := domain.Tracking(carrierCode, trackingCode)
	if err != nil {
		return domain.Shipment{}, err
	}
	p, err := s.shipping.Lookup(ctx, caller, carrier.Carrier(c), code)
	err = shippingError(err, domain.ErrShipmentUnknown)
	if errors.Is(err, domain.ErrCarrierNotLinked) {
		if orderErr := s.checkSeller(ctx, caller, order); orderErr != nil {
			return domain.Shipment{}, orderErr
		}
	}
	if err != nil {
		return domain.Shipment{}, err
	}
	return domain.Shipment{
		Mode: domain.ModeManual, Carrier: c, TrackingCode: code, Fee: p.Fee, Status: domain.ShipmentStatus(p.Status()),
	}, nil
}

// deliveryNotes tell the carrier's shipper what the recipient's place is,
// so an office gets its parcel in office hours. other says nothing.
var deliveryNotes = map[domain.AddressKind]string{
	domain.AddressHome: "Nhà riêng",
	domain.AddressWork: "Địa chỉ công ty: giao giờ hành chính",
}

// place is a recipient or a pickup by names, as the carriers take them.
func (s *Deals) place(ctx context.Context, name, phone, street, commune string) (carrier.Place, error) {
	ps, err := s.catalog.Places(ctx, []string{commune})
	if err != nil {
		return carrier.Place{}, fmt.Errorf("read commune %s: %w", commune, err)
	}
	loc := ps[commune]
	return carrier.Place{Name: name, Phone: phone, Street: street, Commune: loc.Commune.DisplayName, Province: loc.Province.DisplayName}, nil
}

// pickupFrom is the pickup a seller asked for: a carrier's pickup point, a
// copy of one of the seller's addresses, or nil for the carrier's default.
func (s *Deals) pickupFrom(ctx context.Context, seller uuid.UUID, point string, address uuid.UUID) (*domain.ShipmentPickup, error) {
	switch {
	case point != "" && address != uuid.Nil():
		return nil, domain.ErrInvalidShipment.WithField("pickup_from", apperr.OneOf, "carrier_pickup_id or address_id, not both")
	case point != "":
		return &domain.ShipmentPickup{PointID: point}, nil
	case address != uuid.Nil():
		a, err := s.accounts.Address(ctx, seller, address)
		if err != nil {
			return nil, fmt.Errorf("read the pickup address: %w", err)
		}
		return &domain.ShipmentPickup{Name: a.Name, Phone: a.Phone, Street: a.Street, CommuneCode: a.CommuneCode}, nil
	}
	return nil, nil //nolint:nilnil // No pickup asked for: the carrier's default point.
}

func (s *Deals) OpenShipmentsAt(ctx context.Context, seller uuid.UUID, c string) (bool, error) {
	open, err := s.deals.OpenShipmentsAt(ctx, seller, c)
	if err != nil {
		return false, fmt.Errorf("open shipments: %w", err)
	}
	return open, nil
}

// QuoteFacts reads the caller's order as its seller; another's order, or
// one the caller bought, is domain.ErrOrderNotFound.
func (s *Deals) QuoteFacts(ctx context.Context, caller, order, address uuid.UUID) (to carrier.Place, from *carrier.Place, price int64, err error) {
	r, err := s.deals.Order(ctx, order)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && r.SellerID != caller) {
		return carrier.Place{}, nil, 0, domain.ErrOrderNotFound
	}
	if err != nil {
		return carrier.Place{}, nil, 0, fmt.Errorf("get order: %w", err)
	}
	rc := r.Order.Recipient
	to, err = s.place(ctx, rc.Name, rc.Phone, rc.Street, rc.CommuneCode)
	if err != nil {
		return carrier.Place{}, nil, 0, err
	}
	picked, err := s.pickupFrom(ctx, caller, "", address)
	if err != nil || picked == nil {
		return to, nil, r.Order.Price, err
	}
	p, err := s.place(ctx, picked.Name, picked.Phone, picked.Street, picked.CommuneCode)
	if err != nil {
		return carrier.Place{}, nil, 0, err
	}
	return to, &p, r.Order.Price, nil
}
