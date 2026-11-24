package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/provider/carrier"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
)

// The tool names the record pages' reads are recorded under in
// console_agent_reads, beside the MCP tools'.
const (
	readOrderPage   = "console_order"
	readListingPage = "console_listing"
)

// ConsoleOrder is the ConsoleOrder schema.
type ConsoleOrder struct {
	ID           uuid.UUID               `json:"id"`
	Code         string                  `json:"code"`
	Status       dealdomain.OrderStatus  `json:"status"`
	Price        int64                   `json:"price"`
	CreatedAt    time.Time               `json:"created_at"`
	CompletedBy  dealdomain.Party        `json:"completed_by,omitempty"`
	CancelledBy  dealdomain.Party        `json:"cancelled_by,omitempty"`
	CancelReason dealdomain.CancelReason `json:"cancel_reason,omitempty"`
	ShippedAt    *time.Time              `json:"shipped_at,omitempty"`
	ClosedAt     *time.Time              `json:"closed_at,omitempty"`
	Listing      OrderListing            `json:"listing"`
	Buyer        OrderBuyer              `json:"buyer"`
	Seller       OrderSeller             `json:"seller"`
	Shipment     *OrderShipment          `json:"shipment,omitempty"`
	CarrierNow   *CarrierNow             `json:"carrier_now,omitempty"`
	CarrierError string                  `json:"carrier_error,omitempty"`
	Tickets      []OrderTicket           `json:"tickets"`
}

// OrderListing is the listing an order is of.
type OrderListing struct {
	ID           uuid.UUID               `json:"id"`
	Title        string                  `json:"title"`
	PhotoURL     string                  `json:"photo_url,omitempty"`
	Status       catalogdomain.Status    `json:"status"`
	CategoryCode string                  `json:"category_code"`
	Category     string                  `json:"category"`
	Condition    catalogdomain.Condition `json:"condition"`
}

// OrderBuyer is an order's buyer.
type OrderBuyer struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	Since       time.Time `json:"since"`
}

// OrderSeller is an order's seller, with their sold listings and rating.
type OrderSeller struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	Orders      int       `json:"orders"`
	Rating      *Rating   `json:"rating,omitempty"`
}

// Rating is the average of a seller's reviews.
type Rating struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
}

// OrderShipment is an order's shipment, without its pickup.
type OrderShipment struct {
	Mode         string     `json:"mode"`
	Carrier      string     `json:"carrier"`
	TrackingCode string     `json:"tracking_code"`
	Status       string     `json:"status"`
	Fee          *int64     `json:"fee,omitempty"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty"`
}

// CarrierNow is what the carrier says of a parcel now.
type CarrierNow struct {
	Status string `json:"status"`
	Fee    *int64 `json:"fee,omitempty"`
}

// OrderTicket is a support ticket about an order.
type OrderTicket struct {
	ID                uuid.UUID `json:"id"`
	PersonDisplayName string    `json:"person_display_name"`
	Kind              string    `json:"kind"`
	Status            string    `json:"status"`
	LastLine          string    `json:"last_line,omitempty"`
	OpenedAt          time.Time `json:"opened_at"`
}

// ConsoleListing is the ConsoleListing schema.
type ConsoleListing struct {
	ID           uuid.UUID               `json:"id"`
	Title        string                  `json:"title"`
	Price        int64                   `json:"price"`
	Description  string                  `json:"description"`
	CategoryCode string                  `json:"category_code"`
	Category     string                  `json:"category"`
	Condition    catalogdomain.Condition `json:"condition"`
	Status       catalogdomain.Status    `json:"status"`
	AllowOffers  bool                    `json:"allow_offers"`
	Commune      string                  `json:"commune"`
	Province     string                  `json:"province"`
	PhotoURLs    []string                `json:"photo_urls"`
	Specs        []ListingSpec           `json:"specs"`
	CreatedAt    time.Time               `json:"created_at"`
	PublishedAt  *time.Time              `json:"published_at,omitempty"`
	SoldAt       *time.Time              `json:"sold_at,omitempty"`
	Seller       ListingSeller           `json:"seller"`
	Hidden       *ListingHold            `json:"hidden,omitempty"`
	ReportCaseID *uuid.UUID              `json:"report_case_id,omitempty"`
	Reports      []ListingReport         `json:"reports"`
	PriceGuide   *PriceGuide             `json:"price_guide,omitempty"`
}

// ListingSpec is one of a listing's specs.
type ListingSpec struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ListingSeller is a listing's seller with their counts.
type ListingSeller struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	Listings    int       `json:"listings"`
	Sold        int       `json:"sold"`
}

// ListingHold is the member who hid a listing, and when.
type ListingHold struct {
	ByMemberName string    `json:"by_member_name"`
	At           time.Time `json:"at"`
}

// ListingReport is a report about a listing.
type ListingReport struct {
	ReporterDisplayName string    `json:"reporter_display_name"`
	Reason              string    `json:"reason"`
	Note                string    `json:"note"`
	At                  time.Time `json:"at"`
}

// PriceGuide is a listing's price against similar listings'.
type PriceGuide struct {
	Median     int64   `json:"median"`
	SampleSize int     `json:"sample_size"`
	PctVsGuide float64 `json:"pct_vs_guide"`
}

// Order reads an order for the console's order page: the facts the MCP
// tool order_get reads, the parties' accounts, the seller's sold listings
// and rating, and the support tickets about the order, whose last lines it
// records as read for caller.
func (s *Agent) Order(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConsoleOrder, error) {
	f, ok, err := s.deals.Order(ctx, id)
	if err != nil {
		return ConsoleOrder{}, fmt.Errorf("read the order: %w", err)
	}
	if !ok {
		return ConsoleOrder{}, dealdomain.ErrOrderNotFound
	}
	l, _, err := s.catalog.Summary(ctx, f.ListingID)
	if err != nil {
		return ConsoleOrder{}, fmt.Errorf("read the listing: %w", err)
	}
	category, err := s.categoryName(ctx, l.CategoryCode)
	if err != nil {
		return ConsoleOrder{}, err
	}
	buyer, err := s.accounts.ConsoleAccount(ctx, f.BuyerID)
	if err != nil {
		return ConsoleOrder{}, fmt.Errorf("read the buyer: %w", err)
	}
	seller, err := s.accounts.ConsoleAccount(ctx, f.SellerID)
	if err != nil {
		return ConsoleOrder{}, fmt.Errorf("read the seller: %w", err)
	}
	counts, err := s.catalog.SellerCounts(ctx, f.SellerID)
	if err != nil {
		return ConsoleOrder{}, fmt.Errorf("count the seller's listings: %w", err)
	}
	rating, err := s.trust.Rating(ctx, f.SellerID, trustdomain.RoleSeller)
	if err != nil {
		return ConsoleOrder{}, fmt.Errorf("read the seller's rating: %w", err)
	}
	out := ConsoleOrder{
		ID: id, Code: f.Code, Status: f.Status, Price: f.Price, CreatedAt: f.RequestedAt, CompletedBy: f.CompletedBy,
		CancelledBy: f.CancelledBy, CancelReason: f.CancelReason, ShippedAt: f.ShippedAt, ClosedAt: f.ClosedAt,
		Listing: OrderListing{
			ID: l.ID, Title: l.Title, PhotoURL: l.CoverThumbURL, Status: l.Status, CategoryCode: l.CategoryCode, Category: category,
			Condition: l.Condition,
		},
		Buyer:   OrderBuyer{ID: buyer.ID, DisplayName: buyer.DisplayName, Status: string(buyer.Status), Since: buyer.CreatedAt},
		Seller:  OrderSeller{ID: seller.ID, DisplayName: seller.DisplayName, Status: string(seller.Status), Orders: counts.Sold},
		Tickets: []OrderTicket{},
	}
	if rating.Count > 0 {
		out.Seller.Rating = &Rating{Average: rating.Average, Count: rating.Count}
	}
	// The buyer opens a ticket about an order; the seller's list holds the
	// tickets on orders they sold.
	tickets, err := s.support.TicketsOf(ctx, f.BuyerID)
	if err != nil {
		return ConsoleOrder{}, fmt.Errorf("read the order's tickets: %w", err)
	}
	var read []uuid.UUID
	for i := range tickets {
		tk := &tickets[i]
		if tk.Ticket.Order == nil || tk.Ticket.Order.ID != id {
			continue
		}
		out.Tickets = append(out.Tickets, OrderTicket{
			ID: tk.Ticket.ID, PersonDisplayName: tk.Person.DisplayName, Kind: string(tk.Ticket.Kind), Status: string(tk.Ticket.Status),
			LastLine: tk.LastText, OpenedAt: tk.Ticket.OpenedAt,
		})
		read = append(read, tk.Ticket.ID)
	}
	if len(read) > 0 {
		if err = s.RecordRead(ctx, caller.MemberID, readOrderPage, domain.SubjectTicket, read); err != nil {
			return ConsoleOrder{}, err
		}
	}
	sh := f.Shipment
	if sh == nil {
		return out, nil
	}
	// The pickup stays out: it may be a copy of the seller's address.
	out.Shipment = &OrderShipment{
		Mode: string(sh.Mode), Carrier: string(sh.Carrier), TrackingCode: sh.TrackingCode, Status: string(sh.Status), Fee: sh.Fee,
		DeliveredAt: sh.DeliveredAt,
	}
	if sh.Mode != dealdomain.ModeLinked {
		return out, nil
	}
	// The recipient's name the carrier returns stays out too.
	p, err := s.shipping.Lookup(ctx, f.SellerID, carrier.Carrier(sh.Carrier), sh.TrackingCode)
	if err != nil {
		out.CarrierError = apperr.From(err).Code
		return out, nil
	}
	out.CarrierNow = &CarrierNow{Status: string(p.Status()), Fee: p.Fee}
	return out, nil
}

// Listing reads a listing for the console's listing page: the facts the
// MCP tool listing_get reads, as its seller sees it, with the seller's
// counts, the member who hid it, the reports about it in the seller's
// report case, which it records as read for caller, and its price guide.
func (s *Agent) Listing(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConsoleListing, error) {
	sum, ok, err := s.catalog.Summary(ctx, id)
	if err != nil {
		return ConsoleListing{}, fmt.Errorf("read the listing: %w", err)
	}
	if !ok {
		return ConsoleListing{}, catalogdomain.ErrNotFound
	}
	// Read as its seller reads it, so a hidden or sold listing shows too.
	l, err := s.catalog.Get(ctx, &sum.SellerID, id, locale.Vietnamese)
	if err != nil {
		return ConsoleListing{}, fmt.Errorf("read the listing: %w", err)
	}
	category, err := s.categoryName(ctx, l.CategoryCode)
	if err != nil {
		return ConsoleListing{}, err
	}
	seller, err := s.accounts.ConsoleAccount(ctx, l.SellerID)
	if err != nil {
		return ConsoleListing{}, fmt.Errorf("read the seller: %w", err)
	}
	counts, err := s.catalog.SellerCounts(ctx, l.SellerID)
	if err != nil {
		return ConsoleListing{}, fmt.Errorf("count the seller's listings: %w", err)
	}
	out := ConsoleListing{
		ID: l.ID, Title: l.Title, Price: l.Price, Description: l.Description, CategoryCode: l.CategoryCode, Category: category,
		Condition: l.Condition, Status: l.Status, AllowOffers: l.AllowOffers,
		Commune: l.Location.Commune.DisplayName, Province: l.Location.Province.DisplayName,
		PhotoURLs: make([]string, 0, len(l.Photos)), Specs: make([]ListingSpec, 0, len(l.Specs)),
		CreatedAt: l.CreatedAt, PublishedAt: l.PublishedAt, SoldAt: l.SoldAt,
		Seller: ListingSeller{
			ID: seller.ID, DisplayName: seller.DisplayName, Status: string(seller.Status), Listings: counts.OnSale, Sold: counts.Sold,
		},
		Reports: []ListingReport{},
	}
	for _, p := range l.Photos {
		if p.URL != "" {
			out.PhotoURLs = append(out.PhotoURLs, p.URL)
		}
	}
	for _, sp := range l.Specs {
		out.Specs = append(out.Specs, ListingSpec{Label: sp.Label, Value: sp.Value})
	}
	if l.Status == catalogdomain.StatusHidden {
		hold, held, holdErr := s.holdOf(ctx, id)
		if holdErr != nil {
			return ConsoleListing{}, holdErr
		}
		if held {
			out.Hidden = &hold
		}
	}
	c, err := s.trust.ReportCase(ctx, l.SellerID)
	switch {
	case errors.Is(err, trustdomain.ErrCaseNotFound):
	case err != nil:
		return ConsoleListing{}, fmt.Errorf("read the seller's report case: %w", err)
	default:
		read := []uuid.UUID{l.SellerID}
		for i := range c.Reports {
			r := &c.Reports[i]
			if r.ListingID != id {
				continue
			}
			out.Reports = append(out.Reports, ListingReport{
				ReporterDisplayName: r.Reporter.DisplayName, Reason: string(r.Reason), Note: r.Note, At: r.CreatedAt,
			})
			read = append(read, r.ID, r.Reporter.ID)
		}
		if len(out.Reports) > 0 {
			out.ReportCaseID = &l.SellerID
			if err = s.RecordRead(ctx, caller.MemberID, readListingPage, domain.SubjectAccount, read); err != nil {
				return ConsoleListing{}, err
			}
		}
	}
	m, ok, err := s.catalog.MarketPrice(ctx, id, s.textMin, s.photoMin)
	if err != nil {
		return ConsoleListing{}, fmt.Errorf("price the listing: %w", err)
	}
	if ok && m.SampleSize > 0 && m.Median > 0 {
		pct := float64(m.Price-m.Median) / float64(m.Median) * 100
		out.PriceGuide = &PriceGuide{Median: m.Median, SampleSize: m.SampleSize, PctVsGuide: math.Round(pct*10) / 10}
	}
	return out, nil
}

// holdOf is the member who hid the listing and when, from the latest
// listing.hide in the audit log; ok is false when an unhide is later, or
// none is there, as for a listing its seller hid.
func (s *Agent) holdOf(ctx context.Context, listing uuid.UUID) (hold ListingHold, ok bool, err error) {
	acts, err := s.console.ListActions(ctx, audit.Filter{TargetID: listing}, page.Request{Limit: holdHistory})
	if err != nil {
		return ListingHold{}, false, fmt.Errorf("read the listing's console actions: %w", err)
	}
	for i := range acts.Items {
		a := &acts.Items[i]
		if a.Action == audit.ListingUnhide {
			return ListingHold{}, false, nil
		}
		if a.Action == audit.ListingHide {
			return ListingHold{ByMemberName: a.Actor.DisplayName, At: a.At}, true, nil
		}
	}
	return ListingHold{}, false, nil
}

// holdHistory is how many of a listing's latest console actions holdOf
// reads.
const holdHistory = 20

// categoryName is the display name of the category code, or the code when
// catalog has no such category.
func (s *Agent) categoryName(ctx context.Context, code string) (string, error) {
	cats, err := s.catalog.Categories(ctx, locale.Vietnamese)
	if err != nil {
		return "", fmt.Errorf("read the categories: %w", err)
	}
	for _, c := range cats {
		if c.Code == code {
			return c.DisplayName, nil
		}
	}
	return code, nil
}
