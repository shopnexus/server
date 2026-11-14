package domain_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

var (
	reserved = domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingReserved, ReservedFor: uuid.NewV7()}
	hidden   = domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingHidden}
	paused   = domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingPaused}
	gone     = domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingGone}
)

var recipient = domain.Recipient{
	Name: "Hân", Phone: "+84912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: domain.AddressWork,
}

// later is a moment after now, for moves that must not take now's time.
var later = now.Add(time.Hour)

// manual is a manual shipment as its carrier reads it: waiting for pickup,
// with a fee.
func manual() domain.Shipment {
	return domain.Shipment{
		Mode: domain.ModeManual, Carrier: domain.CarrierSPX, TrackingCode: "SPXVN0123456789",
		Fee: new(int64(22_000)), Status: domain.ShipmentPendingPickup,
	}
}

// agreed is a deal agreed at the listed price.
func agreed(t *testing.T) domain.Deal {
	t.Helper()
	d := newDeal()
	must(t, d.Accept(domain.Buyer, 1_500_000, onSale, now))
	return d
}

// pending is an agreed deal whose buyer asked for COD.
func pending(t *testing.T) domain.Deal {
	t.Helper()
	d := agreed(t)
	must(t, d.RequestCod(domain.Buyer, recipient, onSale, now))
	return d
}

// shipped is a pending deal the seller shipped manually.
func shipped(t *testing.T) domain.Deal {
	t.Helper()
	d := pending(t)
	must(t, d.Ship(domain.Seller, d.Order().ID, manual(), onSale, now))
	return d
}

// reported is a shipped deal whose carrier reported status.
func reported(t *testing.T, status domain.ShipmentStatus) domain.Deal {
	t.Helper()
	d := shipped(t)
	if _, err := d.ShipmentUpdated(d.Order().ID, status, now); err != nil {
		t.Fatal(err)
	}
	return d
}

// field returns the problem err reports for name.
func field(err error, name string) string {
	if e, ok := errors.AsType[*apperr.Error](err); ok {
		return e.Fields[name]
	}
	return ""
}

// last is the newest pending move.
func last(d *domain.Deal) domain.Move {
	p := d.Pending()
	return p[len(p)-1]
}

func TestTracking(t *testing.T) {
	t.Parallel()
	for _, c := range []domain.Carrier{domain.CarrierGHTK, domain.CarrierGHN, domain.CarrierSPX, domain.CarrierJT, domain.CarrierViettelPost} {
		got, code, err := domain.Tracking(string(c), "  X1 ")
		if err != nil || got != c || code != "X1" {
			t.Errorf("%s: %s %q %v", c, got, code, err)
		}
	}
	if _, code, err := domain.Tracking("ghn", strings.Repeat("ạ", 50)); err != nil || code == "" {
		t.Errorf("50 characters: %v", err)
	}
	for _, c := range []struct{ carrier, code, field string }{
		{"other", "X1", "carrier"},
		{"pigeon", "X1", "carrier"},
		{"ghn", "", "tracking_code"},
		{"ghn", "   ", "tracking_code"},
		{"ghn", strings.Repeat("x", 51), "tracking_code"},
	} {
		if _, _, err := domain.Tracking(c.carrier, c.code); !errors.Is(err, domain.ErrInvalidShipment) || field(err, c.field) == "" {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestNewLinkedDraft(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		carrier, service string
		weight           int
		pickup           string
	}{{"ghtk", "standard", 800, "home"}, {"ghn", "express", 20_000, "post_office"}, {"ghtk", "standard", 1, "home"}} {
		got, err := domain.NewLinkedDraft(c.carrier, c.service, c.weight, c.pickup)
		if err != nil || string(got.Carrier) != c.carrier || string(got.Service) != c.service || got.WeightGrams != c.weight ||
			string(got.Pickup) != c.pickup {
			t.Errorf("%+v: %+v %v", c, got, err)
		}
	}
	for _, c := range []struct {
		carrier, service string
		weight           int
		pickup           string
		field            string
	}{
		{"spx", "standard", 800, "home", "carrier"},
		{"", "standard", 800, "home", "carrier"},
		{"ghtk", "road", 800, "home", "service"},
		{"ghtk", "standard", 0, "home", "weight_grams"},
		{"ghtk", "standard", 20_001, "home", "weight_grams"},
		{"ghtk", "standard", 800, "door", "pickup"},
	} {
		if _, err := domain.NewLinkedDraft(c.carrier, c.service, c.weight, c.pickup); !errors.Is(err, domain.ErrInvalidShipment) || field(err, c.field) == "" {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestParseShipmentStatus(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"pending_pickup", "in_transit", "delivered", "failed"} {
		if got, err := domain.ParseShipmentStatus(s); err != nil || string(got) != s {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{"returned", "", "Delivered"} {
		if _, err := domain.ParseShipmentStatus(s); !errors.Is(err, domain.ErrInvalidStatus) {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestOrderLifecycle(t *testing.T) {
	t.Parallel()
	d := agreed(t)
	if err := d.RequestCod(domain.Seller, recipient, onSale, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("seller requests: %v", err)
	}
	must(t, d.RequestCod(domain.Buyer, recipient, onSale, now))
	o := d.Order()
	if o == nil || o.Status != domain.OrderPending || o.Price != 1_500_000 || !o.Open() || o.Shipment != nil {
		t.Fatalf("%+v", o)
	}
	if err := d.RequestCod(domain.Buyer, recipient, onSale, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("second request: %v", err)
	}
	if err := d.Ship(domain.Buyer, o.ID, manual(), onSale, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("buyer ships: %v", err)
	}
	if err := d.Ship(domain.Seller, o.ID, manual(), reserved, now); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Fatalf("ship a reserved listing: %v", err)
	}
	failed := manual()
	failed.Status = domain.ShipmentFailed
	if err := d.Ship(domain.Seller, o.ID, failed, onSale, now); !errors.Is(err, domain.ErrInvalidShipment) || field(err, "tracking_code") == "" {
		t.Fatalf("ship a failed parcel: %v", err)
	}
	if err := d.ConfirmReceipt(domain.Buyer, o.ID, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("confirm a pending order: %v", err)
	}
	must(t, d.Ship(domain.Seller, o.ID, manual(), onSale, now))
	if o = d.Order(); o.Status != domain.OrderShipped || o.Shipment == nil || o.Shipment.TrackingCode != "SPXVN0123456789" ||
		o.Shipment.DeliveredAt != nil || o.ShippedAt == nil {
		t.Fatalf("%+v", o)
	}
	if m := last(&d); m.Kind != domain.MoveShipped || m.By != domain.Seller || m.Amount != 22_000 {
		t.Fatalf("%+v", m)
	}
	for _, by := range []domain.Party{domain.Buyer, domain.Seller} {
		if err := d.Cancel(by, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Fatalf("%s cancels a shipped order: %v", by, err)
		}
	}
	if err := d.ConfirmReceipt(domain.Seller, o.ID, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("seller confirms: %v", err)
	}
	must(t, d.ConfirmReceipt(domain.Buyer, o.ID, later))
	if o = d.Order(); o.Status != domain.OrderCompleted || o.ClosedAt == nil || !o.ClosedAt.Equal(later) ||
		d.Status() != domain.StatusSold || o.Open() {
		t.Fatalf("%+v %s", o, d.Status())
	}
	if m := last(&d); m.Kind != domain.MoveCompleted || m.By != domain.Buyer {
		t.Fatalf("%+v", m)
	}
	for name, move := range map[string]func() error{
		"offer":   func() error { return d.Offer(domain.Buyer, 1_000_000, onSale, now) },
		"accept":  func() error { return d.Accept(domain.Buyer, 1_500_000, onSale, now) },
		"cancel":  func() error { return d.Cancel(domain.Buyer, now) },
		"request": func() error { return d.RequestCod(domain.Buyer, recipient, onSale, now) },
		"confirm": func() error { return d.ConfirmReceipt(domain.Buyer, o.ID, now) },
	} {
		if err := move(); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Errorf("%s after completion: %v", name, err)
		}
	}
}

func TestShipTakesTheCarriersWord(t *testing.T) {
	t.Parallel()
	d := pending(t)
	s := manual()
	s.Fee = nil
	must(t, d.Ship(domain.Seller, d.Order().ID, s, onSale, now))
	if d.Order().Shipment.Fee != nil || last(&d).Amount != 0 {
		t.Fatalf("a shipment without a fee: %+v %+v", d.Order().Shipment, last(&d))
	}
	d = pending(t)
	s = manual()
	s.Status, s.DeliveredAt = domain.ShipmentDelivered, &later
	must(t, d.Ship(domain.Seller, d.Order().ID, s, onSale, now))
	if sh := d.Order().Shipment; sh.Status != domain.ShipmentDelivered || sh.DeliveredAt == nil || !sh.DeliveredAt.Equal(now) {
		t.Fatalf("a delivered parcel: %+v", sh)
	}
}

func TestShippable(t *testing.T) {
	t.Parallel()
	d := pending(t)
	id := d.Order().ID
	if err := d.Shippable(domain.Seller, id, onSale); err != nil {
		t.Fatalf("the seller: %v", err)
	}
	for name, c := range map[string]struct {
		by    domain.Party
		order uuid.UUID
		l     domain.Listing
		want  error
	}{
		"the buyer":      {domain.Buyer, id, onSale, domain.ErrMoveNotAllowed},
		"another order":  {domain.Seller, uuid.NewV7(), onSale, domain.ErrMoveNotAllowed},
		"a reserved one": {domain.Seller, id, reserved, domain.ErrListingUnavailable},
		"a hidden one":   {domain.Seller, id, hidden, domain.ErrListingUnavailable},
		"a paused one":   {domain.Seller, id, paused, domain.ErrListingUnavailable},
		"a listing gone": {domain.Seller, id, gone, domain.ErrListingUnavailable},
	} {
		if err := d.Shippable(c.by, c.order, c.l); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	s := shipped(t)
	if err := s.Shippable(domain.Seller, s.Order().ID, onSale); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("a shipped order: %v", err)
	}
}

func TestEditShipment(t *testing.T) {
	t.Parallel()
	edited := domain.Shipment{
		Mode: domain.ModeLinked, Carrier: domain.CarrierGHN, TrackingCode: "GHN0042",
		Fee: new(int64(30_000)), Status: domain.ShipmentInTransit,
	}
	for _, status := range []domain.ShipmentStatus{domain.ShipmentPendingPickup, domain.ShipmentInTransit} {
		d := shipped(t)
		if status != domain.ShipmentPendingPickup {
			d = reported(t, status)
		}
		must(t, d.EditShipment(domain.Seller, d.Order().ID, edited, later))
		sh := d.Order().Shipment
		if sh.Mode != domain.ModeManual || sh.Carrier != domain.CarrierGHN || sh.TrackingCode != "GHN0042" ||
			*sh.Fee != 30_000 || sh.Status != domain.ShipmentInTransit || sh.DeliveredAt != nil {
			t.Errorf("from %s: %+v", status, sh)
		}
		if m := last(&d); m.Kind != domain.MoveShipmentUpdated || m.By != domain.Seller {
			t.Errorf("from %s: %+v", status, m)
		}
	}
	d := shipped(t)
	delivered := edited
	delivered.Status = domain.ShipmentDelivered
	must(t, d.EditShipment(domain.Seller, d.Order().ID, delivered, later))
	if sh := d.Order().Shipment; sh.DeliveredAt == nil || !sh.DeliveredAt.Equal(later) {
		t.Fatalf("edited to a delivered parcel: %+v", sh)
	}
	d = shipped(t)
	failed := edited
	failed.Status = domain.ShipmentFailed
	if err := d.EditShipment(domain.Seller, d.Order().ID, failed, later); !errors.Is(err, domain.ErrInvalidShipment) ||
		d.Order().Shipment.TrackingCode != "SPXVN0123456789" {
		t.Fatalf("edited to a failed parcel: %v %+v", err, d.Order().Shipment)
	}
	linked := pending(t)
	l := manual()
	l.Mode, l.Carrier = domain.ModeLinked, domain.CarrierGHTK
	must(t, linked.Ship(domain.Seller, linked.Order().ID, l, onSale, now))
	for name, c := range map[string]struct {
		d     domain.Deal
		by    domain.Party
		order func(domain.Deal) uuid.UUID
	}{
		"the buyer":         {shipped(t), domain.Buyer, func(d domain.Deal) uuid.UUID { return d.Order().ID }},
		"another order":     {shipped(t), domain.Seller, func(domain.Deal) uuid.UUID { return uuid.NewV7() }},
		"a linked shipment": {linked, domain.Seller, func(d domain.Deal) uuid.UUID { return d.Order().ID }},
		"a delivered one":   {reported(t, domain.ShipmentDelivered), domain.Seller, func(d domain.Deal) uuid.UUID { return d.Order().ID }},
		"a pending order":   {pending(t), domain.Seller, func(d domain.Deal) uuid.UUID { return d.Order().ID }},
	} {
		if err := c.d.EditShipment(c.by, c.order(c.d), edited, later); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestConfirmReceiptWhateverTheCarrierSays(t *testing.T) {
	t.Parallel()
	for _, status := range []domain.ShipmentStatus{domain.ShipmentPendingPickup, domain.ShipmentInTransit, domain.ShipmentDelivered} {
		d := shipped(t)
		if status != domain.ShipmentPendingPickup {
			d = reported(t, status)
		}
		if err := d.ConfirmReceipt(domain.Buyer, d.Order().ID, later); err != nil || d.Order().Status != domain.OrderCompleted {
			t.Errorf("%s: %v %+v", status, err, d.Order())
		}
	}
}

func TestShipmentUpdated(t *testing.T) {
	t.Parallel()
	d := shipped(t)
	id := d.Order().ID
	changed, err := d.ShipmentUpdated(id, domain.ShipmentInTransit, now)
	if err != nil || !changed || d.Order().Shipment.Status != domain.ShipmentInTransit {
		t.Fatalf("in transit: %v %v %+v", changed, err, d.Order().Shipment)
	}
	if m := last(&d); m.Kind != domain.MoveShipmentUpdated || m.By != domain.System {
		t.Fatalf("%+v", m)
	}
	moves := len(d.Pending())
	if changed, err = d.ShipmentUpdated(id, domain.ShipmentInTransit, later); err != nil || changed || len(d.Pending()) != moves {
		t.Fatalf("the same status again: %v %v", changed, err)
	}
	must(t, func() error { _, e := d.ShipmentUpdated(id, domain.ShipmentDelivered, later); return e }())
	if sh := d.Order().Shipment; sh.DeliveredAt == nil || !sh.DeliveredAt.Equal(later) || d.Order().Status != domain.OrderShipped {
		t.Fatalf("delivered: %+v %+v", sh, d.Order())
	}
	must(t, func() error { _, e := d.ShipmentUpdated(id, domain.ShipmentInTransit, later); return e }())
	if d.Order().Shipment.DeliveredAt != nil {
		t.Fatal("a parcel back in transit kept its delivery time")
	}
	must(t, func() error { _, e := d.ShipmentUpdated(id, domain.ShipmentFailed, later); return e }())
	o := d.Order()
	if o.Status != domain.OrderFailed || o.ClosedAt == nil || o.Shipment.Status != domain.ShipmentFailed ||
		d.Status() != domain.StatusNegotiating || d.AgreedPrice() != 0 || d.AgreedAt() != nil {
		t.Fatalf("failed: %+v %+v", o, d.State())
	}
	if m := last(&d); m.Kind != domain.MoveFailed || m.By != domain.System {
		t.Fatalf("%+v", m)
	}
	if _, err = d.ShipmentUpdated(id, domain.ShipmentDelivered, later); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("a report for a closed order: %v", err)
	}
	p := pending(t)
	if _, err = p.ShipmentUpdated(p.Order().ID, domain.ShipmentInTransit, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("a pending order: %v", err)
	}
	s := shipped(t)
	if _, err = s.ShipmentUpdated(uuid.NewV7(), domain.ShipmentInTransit, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("another order: %v", err)
	}
}

func TestSellerTimeout(t *testing.T) {
	t.Parallel()
	d := pending(t)
	must(t, d.SellerTimeout(d.Order().ID, later))
	o := d.Order()
	if o.Status != domain.OrderCancelled || o.CancelledBy != domain.System || o.CancelReason != domain.CancelSellerTimeout ||
		d.Status() != domain.StatusNegotiating || last(&d).Kind != domain.MoveOrderCancelled {
		t.Fatalf("%+v %s", o, d.Status())
	}
	cancelled := pending(t)
	must(t, cancelled.Cancel(domain.Buyer, now))
	other := pending(t)
	for name, c := range map[string]struct {
		d     domain.Deal
		order uuid.UUID
	}{
		"a shipped order":   {shipped(t), uuid.UUID{}},
		"a cancelled order": {cancelled, cancelled.Order().ID},
		"another order":     {other, uuid.NewV7()},
	} {
		if c.order == (uuid.UUID{}) {
			c.order = c.d.Order().ID
		}
		if err := c.d.SellerTimeout(c.order, later); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPickupTimeout(t *testing.T) {
	t.Parallel()
	d := shipped(t)
	must(t, d.PickupTimeout(d.Order().ID, later))
	o := d.Order()
	if o.Status != domain.OrderCancelled || o.CancelledBy != domain.System || o.CancelReason != domain.CancelPickupTimeout ||
		o.ClosedAt == nil || o.Shipment == nil || d.Status() != domain.StatusNegotiating || d.AgreedPrice() != 0 {
		t.Fatalf("%+v %s", o, d.Status())
	}
	completed := shipped(t)
	must(t, completed.ConfirmReceipt(domain.Buyer, completed.Order().ID, now))
	for name, d := range map[string]domain.Deal{
		"in transit": reported(t, domain.ShipmentInTransit),
		"delivered":  reported(t, domain.ShipmentDelivered),
		"pending":    pending(t),
		"completed":  completed,
	} {
		if err := d.PickupTimeout(d.Order().ID, later); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAutoComplete(t *testing.T) {
	t.Parallel()
	d := reported(t, domain.ShipmentDelivered)
	must(t, d.AutoComplete(d.Order().ID, later))
	if o := d.Order(); o.Status != domain.OrderCompleted || d.Status() != domain.StatusSold || last(&d).By != domain.System ||
		last(&d).Kind != domain.MoveCompleted {
		t.Fatalf("%+v %s", o, d.Status())
	}
	confirmed := reported(t, domain.ShipmentDelivered)
	must(t, confirmed.ConfirmReceipt(domain.Buyer, confirmed.Order().ID, now))
	for name, d := range map[string]domain.Deal{
		"waiting for pickup": shipped(t),
		"in transit":         reported(t, domain.ShipmentInTransit),
		"confirmed":          confirmed,
	} {
		if err := d.AutoComplete(d.Order().ID, later); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestFailedNegotiatesAgain(t *testing.T) {
	t.Parallel()
	d := reported(t, domain.ShipmentFailed)
	first := d.Order().ID
	must(t, d.Accept(domain.Buyer, 1_500_000, onSale, now))
	must(t, d.RequestCod(domain.Buyer, recipient, onSale, now))
	if d.Order().ID == first || d.Order().Status != domain.OrderPending {
		t.Fatalf("%+v", d.Order())
	}
	if err := d.Ship(domain.Seller, first, manual(), onSale, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("ship the failed order: %v", err)
	}
	must(t, d.Ship(domain.Seller, d.Order().ID, manual(), onSale, now))
	if _, err := d.ShipmentUpdated(first, domain.ShipmentDelivered, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("a report for the failed order: %v", err)
	}
	if err := d.ConfirmReceipt(domain.Buyer, first, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("confirm the failed order: %v", err)
	}
	if d.Order().Status != domain.OrderShipped || d.Order().Shipment.Status != domain.ShipmentPendingPickup {
		t.Fatalf("the second order moved: %+v", d.Order())
	}
}

func TestCancelAPendingOrder(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		by     domain.Party
		reason domain.CancelReason
	}{{domain.Buyer, domain.CancelByBuyer}, {domain.Seller, domain.CancelBySeller}} {
		d := pending(t)
		must(t, d.Cancel(c.by, now))
		o := d.Order()
		if o.Status != domain.OrderCancelled || o.CancelledBy != c.by || o.CancelReason != c.reason ||
			d.Status() != domain.StatusNegotiating || last(&d).Kind != domain.MoveOrderCancelled {
			t.Errorf("%s: %+v %s %v", c.by, o, d.Status(), kinds(&d))
		}
	}
	d := agreed(t)
	must(t, d.Cancel(domain.Buyer, now))
	if k := kinds(&d); k[len(k)-1] != domain.MoveCancel {
		t.Fatalf("%v", k)
	}
}

func TestRequestNeedsAnActiveListing(t *testing.T) {
	t.Parallel()
	for name, l := range map[string]domain.Listing{"reserved": reserved, "hidden": hidden, "paused": paused} {
		d := agreed(t)
		if err := d.RequestCod(domain.Buyer, recipient, l, now); !errors.Is(err, domain.ErrListingUnavailable) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestListingChangedCancelsPendingOrders(t *testing.T) {
	t.Parallel()
	d := pending(t)
	own := domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingReserved, ReservedFor: d.Order().ID}
	if d.ListingChanged(own, now) {
		t.Fatal("a listing reserved for this order cancelled it")
	}
	cheaper := domain.Listing{Price: 1_400_000, AllowOffers: true, Status: domain.ListingActive}
	if d.ListingChanged(cheaper, now) || d.Order().Status != domain.OrderPending {
		t.Fatal("a price change cancelled the order")
	}
	for _, c := range []struct {
		l      domain.Listing
		reason domain.CancelReason
	}{{reserved, domain.CancelShippedToAnother}, {hidden, domain.CancelListingClosed}, {gone, domain.CancelListingClosed}} {
		d := pending(t)
		if !d.ListingChanged(c.l, now) {
			t.Fatalf("%s: unchanged", c.l.Status)
		}
		if o := d.Order(); o.Status != domain.OrderCancelled || o.CancelledBy != domain.System || o.CancelReason != c.reason ||
			d.Status() != domain.StatusNegotiating {
			t.Errorf("%s: %+v %s", c.l.Status, o, d.Status())
		}
	}
	s := shipped(t)
	for _, l := range []domain.Listing{reserved, hidden, gone, onSale} {
		if s.ListingChanged(l, now) {
			t.Errorf("a shipped order changed on %s", l.Status)
		}
	}
}

func TestRecipientFor(t *testing.T) {
	t.Parallel()
	d := pending(t)
	must(t, d.Cancel(domain.Buyer, now))
	o := d.Order()
	if r := o.RecipientFor(domain.Seller); r.Phone != "" || r.Street != "" || r.Name != "Hân" || r.CommuneCode != "00004" ||
		r.Kind != domain.AddressWork {
		t.Fatalf("seller reads a cancelled order: %+v", r)
	}
	if r := o.RecipientFor(domain.Buyer); r.Phone == "" || r.Street == "" {
		t.Fatalf("buyer: %+v", r)
	}
	s := shipped(t)
	if r := s.Order().RecipientFor(domain.Seller); r.Phone == "" || r.Street == "" {
		t.Fatalf("seller reads a shipped order: %+v", r)
	}
}

func TestReservedListingTakesNoPrice(t *testing.T) {
	t.Parallel()
	d := newDeal()
	must(t, d.Offer(domain.Buyer, 1_000_000, onSale, now))
	if err := d.Offer(domain.Seller, 1_200_000, reserved, now); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Fatalf("counter: %v", err)
	}
	if err := d.Accept(domain.Seller, 1_000_000, reserved, now); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Fatalf("accept: %v", err)
	}
	if d.ListingChanged(reserved, now) || d.OpenFor(reserved) == nil {
		t.Fatal("a reservation dropped the open price")
	}
}

// A flow journals a deal as JSON and the next step reads it back whole.
func TestShippedDealRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	d := reported(t, domain.ShipmentDelivered)
	b, err := json.Marshal(d.State())
	if err != nil {
		t.Fatal(err)
	}
	var st domain.DealState
	if err = json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	back := domain.RestoreDeal(st)
	sh := back.Order().Shipment
	if sh == nil || sh.Mode != domain.ModeManual || *sh.Fee != 22_000 || sh.DeliveredAt == nil || !sh.DeliveredAt.Equal(now) {
		t.Fatalf("%s", b)
	}
}

func TestCompletionRecordsWhoCompleted(t *testing.T) {
	t.Parallel()
	auto := reported(t, domain.ShipmentDelivered)
	must(t, auto.AutoComplete(auto.Order().ID, later))
	byBuyer := shipped(t)
	must(t, byBuyer.ConfirmReceipt(domain.Buyer, byBuyer.Order().ID, later))
	if auto.Order().CompletedBy != domain.System || byBuyer.Order().CompletedBy != domain.Buyer {
		t.Fatalf("auto %q, buyer %q", auto.Order().CompletedBy, byBuyer.Order().CompletedBy)
	}
	if open := shipped(t); open.Order().CompletedBy != "" {
		t.Fatalf("an open order: %q", open.Order().CompletedBy)
	}
}
