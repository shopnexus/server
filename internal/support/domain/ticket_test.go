package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/support/domain"
)

func fieldsOf(err error) map[string]string {
	if e, ok := errors.AsType[*apperr.Error](err); ok {
		return e.Fields
	}
	return nil
}

func TestNewTicketChecksEachKind(t *testing.T) {
	t.Parallel()
	order := uuid.NewV7()
	cases := []struct {
		name, kind, carrier, body string
		order                     *uuid.UUID
		field                     string
	}{
		{"a report without its order", "not_received", "", "", nil, "order_id"},
		{"a carrier request without a name", "carrier_request", "  ", "", nil, "carrier_name"},
		{"a carrier name over 50", "carrier_request", strings.Repeat("x", 51), "", nil, "carrier_name"},
		{"other without a body", "other", "", " ", nil, "body"},
		{"an unknown kind", "refund", "", "x", nil, "kind"},
		{"a body over 1000", "other", "", strings.Repeat("ạ", 1001), nil, "body"},
		{"other with an order", "other", "", "x", &order, "order_id"},
	}
	for _, c := range cases {
		_, err := domain.NewTicket(c.kind, c.carrier, c.body, c.order)
		if !errors.Is(err, domain.ErrInvalidTicket) || fieldsOf(err)[c.field] == "" {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	d, err := domain.NewTicket("not_received", "", "", &order)
	if err != nil || d.Kind != domain.KindNotReceived || d.OrderID == nil || *d.OrderID != order {
		t.Fatalf("a report: %+v %v", d, err)
	}
	d, err = domain.NewTicket("carrier_request", " Ninja Van ", " gần nhà ", nil)
	if err != nil || d.CarrierName != "Ninja Van" || d.CarrierKey != "ninja van" || d.Body != "gần nhà" {
		t.Fatalf("a carrier request: %+v %v", d, err)
	}
}

func TestCarrierKeyFoldsMarksCaseAndSpaces(t *testing.T) {
	t.Parallel()
	if a, b := domain.CarrierKey("  Giao Hàng  Nhanh "), domain.CarrierKey("giao hang nhanh"); a != b {
		t.Fatalf("%q != %q", a, b)
	}
	if got := domain.CarrierKey("Đường Sắt"); got != "duong sat" {
		t.Fatalf("got %q", got)
	}
}

func TestMayReport(t *testing.T) {
	t.Parallel()
	buyer, now := uuid.NewV7(), time.Now()
	recent, old := now.Add(-6*24*time.Hour), now.Add(-8*24*time.Hour)
	cases := []struct {
		name string
		f    domain.OrderCheck
		want error
	}{
		{"shipped", domain.OrderCheck{BuyerID: buyer, Status: "shipped"}, nil},
		{"completed by the system 6 days ago", domain.OrderCheck{BuyerID: buyer, Status: "completed", CompletedBy: "system", ClosedAt: &recent}, nil},
		{"completed by the system 8 days ago", domain.OrderCheck{BuyerID: buyer, Status: "completed", CompletedBy: "system", ClosedAt: &old}, domain.ErrNotAllowed},
		{"completed by the buyer", domain.OrderCheck{BuyerID: buyer, Status: "completed", CompletedBy: "buyer", ClosedAt: &recent}, domain.ErrNotAllowed},
		{"pending", domain.OrderCheck{BuyerID: buyer, Status: "pending"}, domain.ErrNotAllowed},
		{"failed", domain.OrderCheck{BuyerID: buyer, Status: "failed"}, domain.ErrNotAllowed},
		{"not the buyer's", domain.OrderCheck{BuyerID: uuid.NewV7(), Status: "shipped"}, domain.ErrOrderNotFound},
	}
	for _, c := range cases {
		if err := domain.MayReport(c.f, buyer, now); !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
