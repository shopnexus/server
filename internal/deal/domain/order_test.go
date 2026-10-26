package domain_test

import (
	"errors"
	"strings"
	"testing"
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

func recipient(t *testing.T) domain.Recipient {
	t.Helper()
	r, err := domain.NewRecipient(" Hân ", "0912345678", " 12 Lý Thường Kiệt ", "00004", true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func waybill(t *testing.T) domain.Waybill {
	t.Helper()
	w, err := domain.NewWaybill(domain.CarrierSPX, "", "SPXVN0123456789", 22_000)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// agreed is a deal agreed at the listed price.
func agreed(t *testing.T) domain.Deal {
	t.Helper()
	d := newDeal()
	must(t, d.Accept(domain.Buyer, 1_500_000, onSale, now))
	return d
}

// requested is an agreed deal whose buyer asked for COD.
func requested(t *testing.T) domain.Deal {
	t.Helper()
	d := agreed(t)
	must(t, d.RequestCod(domain.Buyer, recipient(t), onSale, now))
	return d
}

// shipped is a requested deal the seller shipped.
func shipped(t *testing.T) domain.Deal {
	t.Helper()
	d := requested(t)
	must(t, d.Ship(domain.Seller, waybill(t), onSale, now))
	return d
}

// field returns the problem err reports for name.
func field(err error, name string) string {
	if e, ok := errors.AsType[*apperr.Error](err); ok {
		return e.Fields[name]
	}
	return ""
}

func TestNewRecipient(t *testing.T) {
	t.Parallel()
	r := recipient(t)
	if r.Name != "Hân" || r.Street != "12 Lý Thường Kiệt" || r.Phone == "" || r.CommuneCode != "00004" {
		t.Fatalf("%+v", r)
	}
	for name, c := range map[string]struct {
		name, phone, street string
		exists              bool
	}{
		"name":         {"  ", "0912345678", "12 Lý", true},
		"phone":        {"Hân", "12345", "12 Lý", true},
		"street":       {"Hân", "0912345678", strings.Repeat("a", 201), true},
		"commune_code": {"Hân", "0912345678", "12 Lý", false},
	} {
		_, err := domain.NewRecipient(c.name, c.phone, c.street, "00004", c.exists)
		if !errors.Is(err, domain.ErrInvalidRecipient) || field(err, name) == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := domain.NewRecipient(strings.Repeat("a", 51), "0912345678", "12 Lý", "00004", true); field(err, "name") == "" {
		t.Errorf("a long name: %v", err)
	}
}

func TestNewWaybill(t *testing.T) {
	t.Parallel()
	for _, c := range []domain.Carrier{domain.CarrierGHTK, domain.CarrierGHN, domain.CarrierSPX, domain.CarrierJT, domain.CarrierViettelPost} {
		w, err := domain.NewWaybill(c, "", "X1", 0)
		if err != nil || w.Source != "self" || w.Carrier != c {
			t.Errorf("%s: %+v %v", c, w, err)
		}
	}
	if w, err := domain.NewWaybill(domain.CarrierOther, "Ahamove", "X1", 30_000); err != nil || w.CarrierName != "Ahamove" {
		t.Errorf("other: %+v %v", w, err)
	}
	for _, c := range []struct {
		carrier     domain.Carrier
		name, track string
		fee         int64
		field       string
	}{
		{domain.CarrierOther, "", "X1", 0, "carrier_name"},
		{domain.CarrierGHN, "GHN", "X1", 0, "carrier_name"},
		{"pigeon", "", "X1", 0, "carrier"},
		{domain.CarrierGHN, "", "", 0, "tracking_code"},
		{domain.CarrierGHN, "", strings.Repeat("x", 51), 0, "tracking_code"},
		{domain.CarrierGHN, "", "X1", -1, "fee"},
		{domain.CarrierGHN, "", "X1", 5_000_001, "fee"},
	} {
		_, err := domain.NewWaybill(c.carrier, c.name, c.track, c.fee)
		if !errors.Is(err, domain.ErrInvalidWaybill) || field(err, c.field) == "" {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestOrderLifecycle(t *testing.T) {
	t.Parallel()
	d := agreed(t)
	if err := d.RequestCod(domain.Seller, recipient(t), onSale, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("seller requests: %v", err)
	}
	must(t, d.RequestCod(domain.Buyer, recipient(t), onSale, now))
	o := d.Order()
	if o == nil || o.Status != domain.OrderRequested || o.Price != 1_500_000 || !o.Open() {
		t.Fatalf("%+v", o)
	}
	if err := d.RequestCod(domain.Buyer, recipient(t), onSale, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("second request: %v", err)
	}
	if err := d.Ship(domain.Buyer, waybill(t), onSale, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("buyer ships: %v", err)
	}
	if err := d.Ship(domain.Seller, waybill(t), reserved, now); !errors.Is(err, domain.ErrListingUnavailable) {
		t.Fatalf("ship a reserved listing: %v", err)
	}
	must(t, d.Ship(domain.Seller, waybill(t), onSale, now))
	if o = d.Order(); o.Status != domain.OrderShipped || o.Waybill == nil || o.ShippedAt == nil {
		t.Fatalf("%+v", o)
	}
	if p := d.Pending(); p[len(p)-1].Kind != domain.MoveShipped || p[len(p)-1].Amount != 22_000 {
		t.Fatalf("%+v", p[len(p)-1])
	}
	for _, by := range []domain.Party{domain.Buyer, domain.Seller} {
		if err := d.Cancel(by, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Fatalf("%s cancels a shipped order: %v", by, err)
		}
	}
	if err := d.Return(domain.Buyer, now); !errors.Is(err, domain.ErrMoveNotAllowed) {
		t.Fatalf("buyer returns: %v", err)
	}
	must(t, d.Deliver(domain.Seller, now))
	if o = d.Order(); o.Status != domain.OrderDelivered || o.ClosedAt == nil || d.Status() != domain.StatusSold || o.Open() {
		t.Fatalf("%+v %s", o, d.Status())
	}
	for name, move := range map[string]func() error{
		"offer":   func() error { return d.Offer(domain.Buyer, 1_000_000, onSale, now) },
		"accept":  func() error { return d.Accept(domain.Buyer, 1_500_000, onSale, now) },
		"cancel":  func() error { return d.Cancel(domain.Buyer, now) },
		"request": func() error { return d.RequestCod(domain.Buyer, recipient(t), onSale, now) },
	} {
		if err := move(); !errors.Is(err, domain.ErrMoveNotAllowed) {
			t.Errorf("%s after delivery: %v", name, err)
		}
	}
}

func TestReturnNegotiatesAgain(t *testing.T) {
	t.Parallel()
	d := shipped(t)
	first := d.Order().ID
	must(t, d.Return(domain.Seller, now))
	if d.Order().Status != domain.OrderReturned || d.Status() != domain.StatusNegotiating || d.AgreedPrice() != 0 {
		t.Fatalf("%+v %s", d.Order(), d.Status())
	}
	must(t, d.Accept(domain.Buyer, 1_500_000, onSale, now))
	must(t, d.RequestCod(domain.Buyer, recipient(t), onSale, now))
	if d.Order().ID == first || d.Order().Status != domain.OrderRequested {
		t.Fatalf("%+v", d.Order())
	}
}

func TestCancelARequest(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		by     domain.Party
		reason domain.CancelReason
	}{{domain.Buyer, domain.CancelByBuyer}, {domain.Seller, domain.CancelBySeller}} {
		d := requested(t)
		must(t, d.Cancel(c.by, now))
		o := d.Order()
		if o.Status != domain.OrderCancelled || o.CancelledBy != c.by || o.CancelReason != c.reason ||
			d.Status() != domain.StatusNegotiating || kinds(&d)[len(kinds(&d))-1] != domain.MoveOrderCancelled {
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
		if err := d.RequestCod(domain.Buyer, recipient(t), l, now); !errors.Is(err, domain.ErrListingUnavailable) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestListingChangedCancelsRequests(t *testing.T) {
	t.Parallel()
	d := requested(t)
	own := domain.Listing{Price: 1_500_000, AllowOffers: true, Status: domain.ListingReserved, ReservedFor: d.Order().ID}
	if d.ListingChanged(own, now) {
		t.Fatal("a listing reserved for this order cancelled it")
	}
	cheaper := domain.Listing{Price: 1_400_000, AllowOffers: true, Status: domain.ListingActive}
	if d.ListingChanged(cheaper, now) || d.Order().Status != domain.OrderRequested {
		t.Fatal("a price change cancelled the order")
	}
	for _, c := range []struct {
		l      domain.Listing
		reason domain.CancelReason
	}{{reserved, domain.CancelSentToAnother}, {hidden, domain.CancelListingClosed}, {gone, domain.CancelListingClosed}} {
		d := requested(t)
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
	d := requested(t)
	must(t, d.Cancel(domain.Buyer, now))
	o := d.Order()
	if r := o.RecipientFor(domain.Seller); r.Phone != "" || r.Street != "" || r.Name != "Hân" || r.CommuneCode != "00004" {
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
