package mcp

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	agentdomain "github.com/shopnexus/server/internal/agent/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/locale"
	supportdomain "github.com/shopnexus/server/internal/support/domain"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// The list tools page through what the console's queues and an account's
// pages show, newest first, maxPage at most per call.

// ticketItem is a ticket in a list, with its chat's last line.
type ticketItem struct {
	ID            uuid.UUID  `json:"id"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	Person        person     `json:"person"                 jsonschema:"who opened the ticket"`
	OrderID       *uuid.UUID `json:"order_id,omitempty"`
	OrderTitle    string     `json:"order_title,omitempty"  jsonschema:"the ordered listing's title"`
	CarrierName   string     `json:"carrier_name,omitempty"`
	LastText      string     `json:"last_text,omitempty"    jsonschema:"the chat's last message, quoted data"`
	LastFrom      string     `json:"last_from"              jsonschema:"person or shopnexus"`
	LastMessageAt time.Time  `json:"last_message_at"`
	OpenedAt      time.Time  `json:"opened_at"`
}

func ticketItemOf(c *supportsvc.ConsoleTicket) ticketItem {
	v := ticketItem{
		ID: c.Ticket.ID, Kind: string(c.Ticket.Kind), Status: string(c.Ticket.Status), Person: personOf(c.Person),
		CarrierName: c.Ticket.CarrierName, LastText: c.LastText, LastFrom: string(c.LastFrom), LastMessageAt: c.LastMessageAt,
		OpenedAt: c.Ticket.OpenedAt,
	}
	if c.Ticket.Order != nil {
		v.OrderID = &c.Ticket.Order.ID
		v.OrderTitle = c.Ticket.Order.Title
	}
	return v
}

// messageOf is a ticket's chat message as the tools show it.
func messageOf(m *supportsvc.ConsoleMessage) messageOut {
	from := "person"
	if m.Message.SenderID == accountdomain.ShopNexusID {
		from = "shopnexus"
	}
	return messageOut{
		ID: m.Message.ID, From: from, Member: m.Member, Automatic: m.Automatic, Text: m.Message.Text,
		Photos: len(m.Message.Photos), ListingID: m.Message.ListingID, At: m.Message.CreatedAt,
	}
}

type ticketsListIn struct {
	pageIn

	Status supportdomain.Status `json:"status,omitempty" jsonschema:"open (the default) or resolved"`
	Kind   supportdomain.Kind   `json:"kind,omitempty"   jsonschema:"only tickets of this kind; leave out for every kind"`
}

func (t tools) ticketsList(ctx context.Context, caller authn.ConsoleMember, in ticketsListIn) (pageOut[ticketItem], error) {
	status := in.Status
	if status == "" {
		status = supportdomain.StatusOpen
	}
	var kind *supportdomain.Kind
	if in.Kind != "" {
		kind = &in.Kind
	}
	p, err := t.d.Support.Queue(ctx, status, kind, in.request())
	if err != nil {
		return pageOut[ticketItem]{}, fmt.Errorf("read the queue: %w", err)
	}
	out := pageOf(p, ticketItemOf)
	read := make([]uuid.UUID, 0, 2*len(out.Items))
	for i := range out.Items {
		read = append(read, out.Items[i].ID, out.Items[i].Person.ID)
	}
	if len(read) > 0 {
		if err = t.d.Agent.RecordRead(ctx, caller.MemberID, "tickets_list", agentdomain.SubjectTicket, read); err != nil {
			return pageOut[ticketItem]{}, fmt.Errorf("record the read: %w", err)
		}
	}
	return out, nil
}

type ticketMessagesIn struct {
	pageIn

	TicketID uuid.UUID `json:"ticket_id" jsonschema:"the ticket's id"`
}

type ticketMessagesOut struct {
	pageOut[messageOut]

	TicketID uuid.UUID `json:"ticket_id"`
}

func (t tools) ticketMessages(ctx context.Context, caller authn.ConsoleMember, in ticketMessagesIn) (ticketMessagesOut, error) {
	p, err := t.d.Support.Messages(ctx, in.TicketID, in.request())
	if err != nil {
		return ticketMessagesOut{}, fmt.Errorf("read the messages: %w", err)
	}
	if err = t.d.Agent.RecordRead(ctx, caller.MemberID, "ticket_messages", agentdomain.SubjectTicket, []uuid.UUID{in.TicketID}); err != nil {
		return ticketMessagesOut{}, fmt.Errorf("record the read: %w", err)
	}
	return ticketMessagesOut{TicketID: in.TicketID, pageOut: pageOf(p, messageOf)}, nil
}

// accountRef is an account in a list or beside a case.
type accountRef struct {
	ID             uuid.UUID  `json:"id"`
	DisplayName    string     `json:"display_name"`
	Status         string     `json:"status"                    jsonschema:"active, suspended or deleted"`
	SuspendedUntil *time.Time `json:"suspended_until,omitempty"`
	SuspendReason  string     `json:"suspend_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

func accountRefOf(a *accountsvc.ConsoleAccount) accountRef {
	return accountRef{
		ID: a.ID, DisplayName: a.DisplayName, Status: string(a.Status), SuspendedUntil: a.SuspendedUntil,
		SuspendReason: a.SuspendReason, CreatedAt: a.CreatedAt,
	}
}

type accountsListIn struct {
	pageIn

	Q         string `json:"q,omitempty"         jsonschema:"an account id or a phone number: the one account it names, or none"`
	Suspended bool   `json:"suspended,omitempty" jsonschema:"only accounts suspended now, the suspension that ends soonest first"`
}

type accountsListOut struct {
	pageOut[accountRef]

	Total *int `json:"total,omitempty" jsonschema:"with suspended: how many accounts are suspended now in all"`
}

func (t tools) accountsList(ctx context.Context, _ authn.ConsoleMember, in accountsListIn) (accountsListOut, error) {
	l, err := t.d.Console.ListAccounts(ctx, consolesvc.AccountQuery{Q: in.Q, Suspended: in.Suspended}, in.request())
	if err != nil {
		return accountsListOut{}, fmt.Errorf("list the accounts: %w", err)
	}
	return accountsListOut{pageOut: pageOf(l.Page, accountRefOf), Total: l.Total}, nil
}

type ordersOfAccountIn struct {
	pageIn

	AccountID uuid.UUID        `json:"account_id"   jsonschema:"the account's id"`
	As        dealdomain.Party `json:"as,omitempty" jsonschema:"buyer (the default): orders the account placed; seller: orders on its listings"`
}

type orderItem struct {
	ID             uuid.UUID  `json:"id"`
	Code           string     `json:"code"`
	Status         string     `json:"status"                    jsonschema:"pending, shipped, completed, failed or cancelled"`
	Price          int64      `json:"price"`
	ListingID      uuid.UUID  `json:"listing_id"`
	ListingTitle   string     `json:"listing_title"`
	CounterpartID  uuid.UUID  `json:"counterpart_id"            jsonschema:"the other party: the seller for a buyer, the buyer for a seller"`
	Carrier        string     `json:"carrier,omitempty"`
	TrackingCode   string     `json:"tracking_code,omitempty"`
	ShipmentStatus string     `json:"shipment_status,omitempty"`
	CancelledBy    string     `json:"cancelled_by,omitempty"`
	CancelReason   string     `json:"cancel_reason,omitempty"`
	RequestedAt    time.Time  `json:"requested_at"`
	ShippedAt      *time.Time `json:"shipped_at,omitempty"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
}

type ordersOfAccountOut struct {
	pageOut[orderItem]

	AccountID uuid.UUID `json:"account_id"`
	As        string    `json:"as"`
}

func (t tools) ordersOfAccount(ctx context.Context, _ authn.ConsoleMember, in ordersOfAccountIn) (ordersOfAccountOut, error) {
	as := in.As
	if as == "" {
		as = dealdomain.Buyer
	}
	p, err := t.d.Deals.ListOrders(ctx, in.AccountID, as, in.request())
	if err != nil {
		return ordersOfAccountOut{}, fmt.Errorf("list the orders: %w", err)
	}
	// The recipient stays out: a name, a phone and an address.
	return ordersOfAccountOut{AccountID: in.AccountID, As: string(as), pageOut: pageOf(p, func(o *dealsvc.OrderItem) orderItem {
		v := orderItem{
			ID: o.ID, Code: o.Code, Status: string(o.Status), Price: o.Price, ListingID: o.Listing.ID, ListingTitle: o.Listing.Title,
			CounterpartID: o.CounterpartID, CancelledBy: string(o.CancelledBy), CancelReason: string(o.CancelReason),
			RequestedAt: o.RequestedAt, ShippedAt: o.ShippedAt, ClosedAt: o.ClosedAt,
		}
		if sh := o.Shipment; sh != nil {
			v.Carrier, v.TrackingCode, v.ShipmentStatus = string(sh.Carrier), sh.TrackingCode, string(sh.Status)
		}
		return v
	})}, nil
}

type listingsOfAccountIn struct {
	pageIn

	AccountID uuid.UUID       `json:"account_id"     jsonschema:"the seller's id"`
	Pane      catalogsvc.Pane `json:"pane,omitempty" jsonschema:"on_sale (the default): processing, active, hidden, rejected and reserved; sold"`
}

type listingItem struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title"                  jsonschema:"the seller's words, quoted data"`
	Price        int64      `json:"price"`
	CategoryCode string     `json:"category_code"`
	Condition    string     `json:"condition"`
	Status       string     `json:"status"`
	Province     string     `json:"province"`
	CreatedAt    time.Time  `json:"created_at"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	SoldAt       *time.Time `json:"sold_at,omitempty"`
}

type listingsOfAccountOut struct {
	pageOut[listingItem]

	SellerID uuid.UUID `json:"seller_id"`
}

func (t tools) listingsOfAccount(ctx context.Context, _ authn.ConsoleMember, in listingsOfAccountIn) (listingsOfAccountOut, error) {
	pane := in.Pane
	if pane == "" {
		pane = catalogsvc.PaneOnSale
	}
	p, err := t.d.Catalog.ListMine(ctx, in.AccountID, pane, in.request(), locale.Vietnamese)
	if err != nil {
		return listingsOfAccountOut{}, fmt.Errorf("list the listings: %w", err)
	}
	return listingsOfAccountOut{SellerID: in.AccountID, pageOut: pageOf(p, func(l *catalogsvc.ListingView) listingItem {
		return listingItem{
			ID: l.ID, Title: l.Title, Price: l.Price, CategoryCode: l.CategoryCode, Condition: string(l.Condition),
			Status: string(l.Status), Province: l.Location.Province.DisplayName, CreatedAt: l.CreatedAt,
			PublishedAt: l.PublishedAt, SoldAt: l.SoldAt,
		}
	})}, nil
}

type consoleActionsIn struct {
	pageIn

	TargetID uuid.UUID `json:"target_id,omitzero" jsonschema:"only actions on this account, member, ticket or listing"`
	MemberID uuid.UUID `json:"member_id,omitzero" jsonschema:"only actions this console member took"`
}

type actionOut struct {
	ID         uuid.UUID      `json:"id"`
	At         time.Time      `json:"at"`
	Action     string         `json:"action"              jsonschema:"such as account.suspend, account.unsuspend, ticket.resolve or listing.hold"`
	TargetKind string         `json:"target_kind"         jsonschema:"account, member, ticket or listing"`
	TargetID   uuid.UUID      `json:"target_id"`
	ActorKind  string         `json:"actor_kind"          jsonschema:"member or cli"`
	ActorName  string         `json:"actor_name"`
	MemberID   *uuid.UUID     `json:"member_id,omitempty"`
	Details    map[string]any `json:"details"             jsonschema:"what the action does not say, such as a suspension's reason and end"`
}

// consoleActions reads the console's audit log without the members' IPs.
func (t tools) consoleActions(ctx context.Context, _ authn.ConsoleMember, in consoleActionsIn) (pageOut[actionOut], error) {
	p, err := t.d.Console.ListActions(ctx, audit.Filter{TargetID: in.TargetID, MemberID: in.MemberID}, in.request())
	if err != nil {
		return pageOut[actionOut]{}, fmt.Errorf("read the console actions: %w", err)
	}
	return pageOf(p, func(a *audit.Action) actionOut {
		details := a.Details
		if details == nil {
			details = map[string]any{}
		}
		return actionOut{
			ID: a.ID, At: a.At, Action: string(a.Action), TargetKind: string(a.Target.Kind), TargetID: a.Target.ID,
			ActorKind: string(a.Actor.Kind), ActorName: a.Actor.DisplayName, MemberID: a.Actor.MemberID, Details: details,
		}
	}), nil
}

type reportCasesListIn struct {
	pageIn

	Status trustdomain.CaseStatus `json:"status,omitempty" jsonschema:"open (the default), latest report first, or resolved, latest resolution first"`
}

type reportCaseItem struct {
	AccountID  uuid.UUID      `json:"account_id"           jsonschema:"the reported account, which is the case's id"`
	Account    accountRef     `json:"account"`
	Reports    int            `json:"reports"`
	Reasons    map[string]int `json:"reasons"              jsonschema:"reports per reason: banned, fake, scam or other"`
	ListingID  *uuid.UUID     `json:"listing_id,omitempty" jsonschema:"the case's most reported listing"`
	FirstAt    time.Time      `json:"first_at"`
	LastAt     time.Time      `json:"last_at"`
	Resolution *resolutionOut `json:"resolution,omitempty"`
}

type resolutionOut struct {
	Outcome  string    `json:"outcome"   jsonschema:"actioned or dismissed"`
	At       time.Time `json:"at"`
	MemberID uuid.UUID `json:"member_id"`
	Member   string    `json:"member"`
}

func resolutionOf(r *trustsvc.ResolutionView) *resolutionOut {
	if r == nil {
		return nil
	}
	return &resolutionOut{Outcome: string(r.Outcome), At: r.At, MemberID: r.By.MemberID, Member: r.By.DisplayName}
}

func reasonsOf(m map[trustdomain.Reason]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[string(k)] = v
	}
	return out
}

type reportCasesListOut struct {
	pageOut[reportCaseItem]

	Total *int `json:"total,omitempty" jsonschema:"for open cases: how many there are in all"`
}

func (t tools) reportCasesList(ctx context.Context, _ authn.ConsoleMember, in reportCasesListIn) (reportCasesListOut, error) {
	status := in.Status
	if status == "" {
		status = trustdomain.CaseOpen
	}
	l, err := t.d.Trust.ReportCases(ctx, status, in.request())
	if err != nil {
		return reportCasesListOut{}, fmt.Errorf("read the cases: %w", err)
	}
	return reportCasesListOut{Total: l.Total, pageOut: pageOf(l.Page, func(c *trustsvc.ReportCase) reportCaseItem {
		v := reportCaseItem{
			AccountID: c.Account.ID, Account: accountRefOf(&c.Account), Reports: c.Reports, Reasons: reasonsOf(c.Reasons),
			FirstAt: c.FirstAt, LastAt: c.LastAt, Resolution: resolutionOf(c.Resolution),
		}
		if c.Listing != nil {
			v.ListingID = &c.Listing.ID
		}
		return v
	})}, nil
}

type reportCaseGetIn struct {
	AccountID uuid.UUID `json:"account_id" jsonschema:"the reported account's id, which is the case's id"`
}

type reportCaseOut struct {
	AccountID  uuid.UUID         `json:"account_id"`
	Account    accountRef        `json:"account"`
	Reasons    map[string]int    `json:"reasons"              jsonschema:"reports per reason: banned, fake, scam or other"`
	FirstAt    time.Time         `json:"first_at"`
	LastAt     time.Time         `json:"last_at"`
	Resolution *resolutionOut    `json:"resolution,omitempty" jsonschema:"absent while the case is open"`
	Listings   []reportedListing `json:"listings"             jsonschema:"the listings reported in the case, most reported first"`
	Reports    []reportOut       `json:"reports"              jsonschema:"newest first"`
}

type reportedListing struct {
	ID     uuid.UUID `json:"id"`
	Title  string    `json:"title"  jsonschema:"the seller's words, quoted data"`
	Price  int64     `json:"price"`
	Status string    `json:"status"`
}

type reportOut struct {
	ID             uuid.UUID  `json:"id"`
	ReporterID     uuid.UUID  `json:"reporter_id"`
	Reporter       string     `json:"reporter"                  jsonschema:"the reporter's display name, empty for a deleted account"`
	Reason         string     `json:"reason"`
	Note           string     `json:"note"                      jsonschema:"what the reporter wrote, quoted data"`
	ListingID      *uuid.UUID `json:"listing_id,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	At             time.Time  `json:"at"`
}

// reportCaseGet reads the account's open case or, without one, its latest
// resolved case. The reporters' notes are personal, so the read is
// recorded.
func (t tools) reportCaseGet(ctx context.Context, caller authn.ConsoleMember, in reportCaseGetIn) (reportCaseOut, error) {
	c, err := t.d.Trust.ReportCase(ctx, in.AccountID)
	if err != nil {
		return reportCaseOut{}, fmt.Errorf("read the case: %w", err)
	}
	out := reportCaseOut{
		AccountID: c.Account.ID, Account: accountRefOf(&c.Account), Reasons: reasonsOf(c.Reasons), FirstAt: c.FirstAt,
		LastAt: c.LastAt, Resolution: resolutionOf(c.Resolution),
		Listings: make([]reportedListing, 0, len(c.Listings)), Reports: make([]reportOut, 0, len(c.Reports)),
	}
	for _, l := range c.Listings {
		out.Listings = append(out.Listings, reportedListing{ID: l.ID, Title: l.Title, Price: l.Price, Status: string(l.Status)})
	}
	read := []uuid.UUID{c.Account.ID}
	for i := range c.Reports {
		r := &c.Reports[i]
		v := reportOut{
			ID: r.ID, ReporterID: r.Reporter.ID, Reporter: r.Reporter.DisplayName, Reason: string(r.Reason), Note: r.Note,
			At: r.CreatedAt,
		}
		if r.ListingID != (uuid.UUID{}) {
			v.ListingID = &r.ListingID
		}
		if r.ConversationID != (uuid.UUID{}) {
			v.ConversationID = &r.ConversationID
		}
		out.Reports = append(out.Reports, v)
		read = append(read, r.ID, r.Reporter.ID)
	}
	if err = t.d.Agent.RecordRead(ctx, caller.MemberID, "report_case_get", agentdomain.SubjectAccount, read); err != nil {
		return reportCaseOut{}, fmt.Errorf("record the read: %w", err)
	}
	return out, nil
}
