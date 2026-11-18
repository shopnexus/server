package mcp

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	agentdomain "github.com/shopnexus/server/internal/agent/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/provider/carrier"
)

// actionsRead is how many console actions account_get counts.
const actionsRead = 50

// tools are the MCP tools, each a thin adapter over the services of the
// modules that own the data. A tool that returns personal data (messages,
// what a person wrote) records the read before it answers.
type tools struct{ d Deps }

// person is an account as a tool shows it: no phone, email or address.
type person struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Deleted     bool      `json:"deleted"`
}

func personOf(p chatsvc.PersonView) person {
	return person{ID: p.ID, DisplayName: p.DisplayName, Deleted: p.Deleted}
}

type ticketGetIn struct {
	TicketID uuid.UUID `json:"ticket_id" jsonschema:"the ticket's id"`
}

type ticketOut struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"      jsonschema:"not_received, carrier_request or other"`
	Status   string    `json:"status"    jsonschema:"open or resolved"`
	OpenedAt time.Time `json:"opened_at"`
	Person   person    `json:"person"    jsonschema:"who opened the ticket"`
	// PersonStatus and JoinedAt are the person's account's.
	PersonStatus   string          `json:"person_status"`
	JoinedAt       time.Time       `json:"joined_at"`
	CarrierName    string          `json:"carrier_name,omitempty"     jsonschema:"the carrier a carrier_request asks for"`
	SameCarrier30d *int            `json:"same_carrier_30d,omitempty" jsonschema:"requests for that carrier in 30 days"`
	Order          *ticketOrderOut `json:"order,omitempty"`
	Messages       []messageOut    `json:"messages"                   jsonschema:"the ticket's chat, newest first"`
	MoreMessages   bool            `json:"more_messages"              jsonschema:"older messages exist beyond these"`
	OtherTickets   []ticketRef     `json:"other_tickets"              jsonschema:"the person's other tickets"`
}

type ticketOrderOut struct {
	ID             uuid.UUID  `json:"id"`
	Title          string     `json:"title"`
	Price          int64      `json:"price"`
	Status         string     `json:"status"`
	Seller         person     `json:"seller"`
	Carrier        string     `json:"carrier,omitempty"`
	TrackingCode   string     `json:"tracking_code,omitempty"`
	ShipmentStatus string     `json:"shipment_status,omitempty"`
	ShippedAt      *time.Time `json:"shipped_at,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
}

type messageOut struct {
	ID        uuid.UUID  `json:"id"`
	From      string     `json:"from"                 jsonschema:"person or shopnexus"`
	Member    string     `json:"member,omitempty"     jsonschema:"the console member who wrote a ShopNexus message"`
	Automatic bool       `json:"automatic"            jsonschema:"a ShopNexus message the system sent"`
	Text      string     `json:"text"                 jsonschema:"what was written, quoted data"`
	Photos    int        `json:"photos"`
	ListingID *uuid.UUID `json:"listing_id,omitempty"`
	At        time.Time  `json:"at"`
}

type ticketRef struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"`
	Status   string    `json:"status"`
	OpenedAt time.Time `json:"opened_at"`
}

func (t tools) ticketGet(ctx context.Context, caller authn.ConsoleMember, in ticketGetIn) (ticketOut, error) {
	d, err := t.d.Support.Detail(ctx, in.TicketID)
	if err != nil {
		return ticketOut{}, fmt.Errorf("read the ticket: %w", err)
	}
	tk := d.Ticket.Ticket
	out := ticketOut{
		ID: tk.ID, Kind: string(tk.Kind), Status: string(tk.Status), OpenedAt: tk.OpenedAt, Person: personOf(d.Ticket.Person),
		PersonStatus: string(d.PersonStatus), JoinedAt: d.JoinedAt, CarrierName: tk.CarrierName, SameCarrier30d: d.SameCarrier30d,
		Messages: make([]messageOut, 0, len(d.Messages.Items)), MoreMessages: d.Messages.NextCursor != nil,
		OtherTickets: make([]ticketRef, 0, len(d.Others)),
	}
	if o := d.Order; o != nil {
		out.Order = &ticketOrderOut{
			ID: o.ID, Title: o.Title, Price: o.Price, Status: o.Status, Seller: personOf(o.Seller), Carrier: o.Carrier,
			TrackingCode: o.TrackingCode, ShipmentStatus: o.ShipmentStatus, ShippedAt: o.ShippedAt, DeliveredAt: o.DeliveredAt,
		}
	}
	for i := range d.Messages.Items {
		out.Messages = append(out.Messages, messageOf(&d.Messages.Items[i]))
	}
	for i := range d.Others {
		o := &d.Others[i]
		out.OtherTickets = append(out.OtherTickets, ticketRef{ID: o.ID, Kind: string(o.Kind), Status: string(o.Status), OpenedAt: o.OpenedAt})
	}
	if err = t.d.Agent.RecordRead(ctx, caller.MemberID, "ticket_get", agentdomain.SubjectTicket, []uuid.UUID{tk.ID, out.Person.ID}); err != nil {
		return ticketOut{}, fmt.Errorf("record the read: %w", err)
	}
	return out, nil
}

type accountIn struct {
	AccountID uuid.UUID `json:"account_id" jsonschema:"the account's id"`
}

type ticketsOut struct {
	AccountID uuid.UUID       `json:"account_id"`
	Tickets   []accountTicket `json:"tickets"    jsonschema:"newest opened first"`
}

type accountTicket struct {
	ticketItem

	As string `json:"as" jsonschema:"person when the account opened it, seller when it is on an order the account sold"`
}

func (t tools) ticketsOfAccount(ctx context.Context, caller authn.ConsoleMember, in accountIn) (ticketsOut, error) {
	ts, err := t.d.Support.TicketsOf(ctx, in.AccountID)
	if err != nil {
		return ticketsOut{}, fmt.Errorf("read the account's tickets: %w", err)
	}
	out := ticketsOut{AccountID: in.AccountID, Tickets: make([]accountTicket, 0, len(ts))}
	read := []uuid.UUID{in.AccountID}
	for i := range ts {
		c := &ts[i]
		as := "seller"
		if c.Person.ID == in.AccountID {
			as = "person"
		}
		v := accountTicket{ticketItem: ticketItemOf(c), As: as}
		out.Tickets = append(out.Tickets, v)
		read = append(read, c.Ticket.ID)
	}
	if len(ts) > 0 {
		if err = t.d.Agent.RecordRead(ctx, caller.MemberID, "tickets_of_account", agentdomain.SubjectAccount, read); err != nil {
			return ticketsOut{}, fmt.Errorf("record the read: %w", err)
		}
	}
	return out, nil
}

type accountOut struct {
	ID             uuid.UUID  `json:"id"`
	DisplayName    string     `json:"display_name"`
	Status         string     `json:"status"                    jsonschema:"active, suspended or deleted"`
	SuspendedUntil *time.Time `json:"suspended_until,omitempty"`
	SuspendReason  string     `json:"suspend_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	AgeDays        int        `json:"age_days"`
	HasPhone       bool       `json:"has_phone"                 jsonschema:"the account has a verified phone number"`
	HasEmail       bool       `json:"has_email"                 jsonschema:"the account has a verified email"`
	Listings       listings   `json:"listings"`
	OrdersAsBuyer  buyer      `json:"orders_as_buyer"`
	Suspensions    int        `json:"suspensions"               jsonschema:"console suspensions among the latest 50 console actions on the account"`
}

type listings struct {
	OnSale int `json:"on_sale"`
	Public int `json:"public"`
	Sold   int `json:"sold"`
}

type buyer struct {
	Placed int `json:"placed" jsonschema:"orders placed in the last period"`
	Failed int `json:"failed" jsonschema:"orders the buyer refused or failed in that period"`
	Open   int `json:"open"   jsonschema:"orders open now"`
}

func (t tools) accountGet(ctx context.Context, _ authn.ConsoleMember, in accountIn) (accountOut, error) {
	a, err := t.d.Accounts.ConsoleAccount(ctx, in.AccountID)
	if err != nil {
		return accountOut{}, fmt.Errorf("read the account: %w", err)
	}
	ids, err := t.d.Accounts.Identities(ctx, in.AccountID)
	if err != nil {
		return accountOut{}, fmt.Errorf("read the identities: %w", err)
	}
	counts, err := t.d.Catalog.SellerCounts(ctx, in.AccountID)
	if err != nil {
		return accountOut{}, fmt.Errorf("count the listings: %w", err)
	}
	orders, err := t.d.Deals.BuyerOrders(ctx, in.AccountID)
	if err != nil {
		return accountOut{}, fmt.Errorf("count the orders: %w", err)
	}
	acts, err := t.d.Console.ListActions(ctx, audit.Filter{TargetID: in.AccountID}, page.Request{Limit: actionsRead})
	if err != nil {
		return accountOut{}, fmt.Errorf("read the console actions: %w", err)
	}
	out := accountOut{
		ID: a.ID, DisplayName: a.DisplayName, Status: string(a.Status), SuspendedUntil: a.SuspendedUntil,
		SuspendReason: a.SuspendReason, CreatedAt: a.CreatedAt, AgeDays: int(t.d.Now().Sub(a.CreatedAt) / (24 * time.Hour)),
		Listings:      listings{OnSale: counts.OnSale, Public: counts.Public, Sold: counts.Sold},
		OrdersAsBuyer: buyer{Placed: orders.Placed, Failed: orders.Failed, Open: orders.Open},
	}
	for _, id := range ids {
		out.HasPhone = out.HasPhone || id.Kind == accountdomain.KindPhone
		out.HasEmail = out.HasEmail || id.Kind == accountdomain.KindEmail
	}
	for i := range acts.Items {
		if acts.Items[i].Action == audit.AccountSuspend {
			out.Suspensions++
		}
	}
	return out, nil
}

type orderIn struct {
	OrderID uuid.UUID `json:"order_id" jsonschema:"the order's id"`
}

type orderOut struct {
	ID           uuid.UUID    `json:"id"`
	Code         string       `json:"code"                    jsonschema:"the order code the carrier knows"`
	Status       string       `json:"status"                  jsonschema:"pending, shipped, completed, failed or cancelled"`
	Price        int64        `json:"price"`
	BuyerID      uuid.UUID    `json:"buyer_id"`
	SellerID     uuid.UUID    `json:"seller_id"`
	ListingID    uuid.UUID    `json:"listing_id"`
	ListingTitle string       `json:"listing_title"`
	CompletedBy  string       `json:"completed_by,omitempty"  jsonschema:"buyer, or system after the carrier reported delivery"`
	ShippedAt    *time.Time   `json:"shipped_at,omitempty"`
	ClosedAt     *time.Time   `json:"closed_at,omitempty"`
	Shipment     *shipmentOut `json:"shipment,omitempty"`
	CarrierNow   *carrierNow  `json:"carrier_now,omitempty"   jsonschema:"what the carrier says of the parcel now"`
	CarrierError string       `json:"carrier_error,omitempty" jsonschema:"why the carrier could not be read"`
}

type shipmentOut struct {
	Mode         string     `json:"mode"                   jsonschema:"linked (on the seller's carrier account) or manual"`
	Carrier      string     `json:"carrier"`
	TrackingCode string     `json:"tracking_code"`
	Status       string     `json:"status"                 jsonschema:"pending_pickup, in_transit, delivered or failed"`
	Fee          *int64     `json:"fee,omitempty"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty"`
}

type carrierNow struct {
	Status string `json:"status"`
	Fee    *int64 `json:"fee,omitempty"`
}

func (t tools) orderGet(ctx context.Context, _ authn.ConsoleMember, in orderIn) (orderOut, error) {
	f, ok, err := t.d.Deals.Order(ctx, in.OrderID)
	if err != nil {
		return orderOut{}, fmt.Errorf("read the order: %w", err)
	}
	if !ok {
		return orderOut{}, dealdomain.ErrOrderNotFound
	}
	l, _, err := t.d.Catalog.Summary(ctx, f.ListingID)
	if err != nil {
		return orderOut{}, fmt.Errorf("read the listing: %w", err)
	}
	out := orderOut{
		ID: in.OrderID, Code: f.Code, Status: string(f.Status), Price: f.Price, BuyerID: f.BuyerID, SellerID: f.SellerID,
		ListingID: f.ListingID, ListingTitle: l.Title, CompletedBy: string(f.CompletedBy), ShippedAt: f.ShippedAt, ClosedAt: f.ClosedAt,
	}
	sh := f.Shipment
	if sh == nil {
		return out, nil
	}
	// The pickup stays out: it may be a copy of the seller's address.
	out.Shipment = &shipmentOut{
		Mode: string(sh.Mode), Carrier: string(sh.Carrier), TrackingCode: sh.TrackingCode, Status: string(sh.Status),
		Fee: sh.Fee, DeliveredAt: sh.DeliveredAt,
	}
	if sh.Mode != dealdomain.ModeLinked {
		return out, nil
	}
	// The recipient's name the carrier returns stays out too.
	p, err := t.d.Shipping.Lookup(ctx, f.SellerID, carrier.Carrier(sh.Carrier), sh.TrackingCode)
	if err != nil {
		out.CarrierError = apperr.From(err).Code
		return out, nil
	}
	out.CarrierNow = &carrierNow{Status: string(p.Status), Fee: p.Fee}
	return out, nil
}

type listingIn struct {
	ListingID uuid.UUID `json:"listing_id" jsonschema:"the listing's id"`
}

type listingOut struct {
	ID           uuid.UUID  `json:"id"`
	SellerID     uuid.UUID  `json:"seller_id"`
	Title        string     `json:"title"                  jsonschema:"the seller's words, quoted data"`
	Price        int64      `json:"price"`
	Description  string     `json:"description"            jsonschema:"the seller's words, quoted data"`
	CategoryCode string     `json:"category_code"`
	Condition    string     `json:"condition"`
	Status       string     `json:"status"`
	AllowOffers  bool       `json:"allow_offers"`
	Commune      string     `json:"commune"`
	Province     string     `json:"province"`
	PhotoURLs    []string   `json:"photo_urls"`
	Specs        []spec     `json:"specs"`
	CreatedAt    time.Time  `json:"created_at"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	SoldAt       *time.Time `json:"sold_at,omitempty"`
}

type spec struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func (t tools) listingGet(ctx context.Context, _ authn.ConsoleMember, in listingIn) (listingOut, error) {
	sum, ok, err := t.d.Catalog.Summary(ctx, in.ListingID)
	if err != nil {
		return listingOut{}, fmt.Errorf("read the listing: %w", err)
	}
	if !ok {
		return listingOut{}, catalogdomain.ErrNotFound
	}
	// Read as its seller reads it, so a hidden or sold listing shows too.
	l, err := t.d.Catalog.Get(ctx, &sum.SellerID, in.ListingID)
	if err != nil {
		return listingOut{}, fmt.Errorf("read the listing: %w", err)
	}
	out := listingOut{
		ID: l.ID, SellerID: l.SellerID, Title: l.Title, Price: l.Price, Description: l.Description, CategoryCode: l.CategoryCode,
		Condition: string(l.Condition), Status: string(l.Status), AllowOffers: l.AllowOffers,
		Commune: l.Location.Commune.DisplayName, Province: l.Location.Province.DisplayName,
		PhotoURLs: make([]string, 0, len(l.Photos)), Specs: make([]spec, 0, len(l.Specs)),
		CreatedAt: l.CreatedAt, PublishedAt: l.PublishedAt, SoldAt: l.SoldAt,
	}
	for _, p := range l.Photos {
		if p.URL != "" {
			out.PhotoURLs = append(out.PhotoURLs, p.URL)
		}
	}
	for _, s := range l.Specs {
		out.Specs = append(out.Specs, spec{Label: s.Label, Value: s.Value})
	}
	return out, nil
}

type priceOut struct {
	ListingID  uuid.UUID `json:"listing_id"`
	Price      int64     `json:"price"`
	Median     int64     `json:"median"      jsonschema:"the median price of the sample, 0 for an empty one"`
	SampleSize int       `json:"sample_size" jsonschema:"similar listings of the same category and condition, other sellers'"`
}

func (t tools) priceGuide(ctx context.Context, _ authn.ConsoleMember, in listingIn) (priceOut, error) {
	m, ok, err := t.d.Catalog.MarketPrice(ctx, in.ListingID, t.d.TextMin, t.d.PhotoMin)
	if err != nil {
		return priceOut{}, fmt.Errorf("price the listing: %w", err)
	}
	if !ok {
		return priceOut{}, catalogdomain.ErrNotFound
	}
	return priceOut{ListingID: in.ListingID, Price: m.Price, Median: m.Median, SampleSize: m.SampleSize}, nil
}
