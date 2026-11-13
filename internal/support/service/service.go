// Package service holds module support's use cases: a person opens a
// ticket, which is its own chat with ShopNexus, and reads their tickets;
// console members work the queue, answer as ShopNexus, resolve and reopen.
// Opening runs on Restate, in ticket_open_durable.go.
package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
)

// Service is module support's contract.
type Service interface {
	// Open opens a ticket of account's under key, or returns the ticket the
	// key, or for not_received the order, has already, with existed true.
	Open(ctx context.Context, account, key uuid.UUID, in OpenInput) (TicketView, bool, error)
	// Mine lists account's tickets, open first, each with its chat's last
	// message and account's unread count.
	Mine(ctx context.Context, account uuid.UUID) ([]TicketListItem, error)
	// Summary counts account's open tickets and unread replies.
	Summary(ctx context.Context, account uuid.UUID) (SummaryView, error)
	// OfOrder reads account's ticket of an order; domain.ErrNotFound for none.
	OfOrder(ctx context.Context, account, order uuid.UUID) (TicketView, error)
	// OfConversation reads the ticket of a support chat of account's; false
	// for any other chat.
	OfConversation(ctx context.Context, account, conversation uuid.UUID) (TicketView, bool, error)
	// Messaged records a message of a support chat at at: the person's
	// message reopens a resolved ticket, ShopNexus's marks it answered.
	Messaged(ctx context.Context, conversation uuid.UUID, fromPerson bool, at time.Time) error
	// AccountDeleted resolves a deleted account's open tickets.
	AccountDeleted(ctx context.Context, account uuid.UUID, at time.Time) error
	// Queue lists tickets of status, of kind when kind is set, newest
	// activity first, for console members.
	Queue(ctx context.Context, status domain.Status, kind *domain.Kind, p page.Request) (page.Page[ConsoleTicket], error)
	// Detail reads a ticket with its person, order, other tickets and the
	// first page of its chat.
	Detail(ctx context.Context, id uuid.UUID) (ConsoleTicketDetail, error)
	// Messages reads a page of a ticket's chat, each ShopNexus message with
	// the member who wrote it.
	Messages(ctx context.Context, id uuid.UUID, p page.Request) (page.Page[ConsoleMessage], error)
	// Reply sends a message as ShopNexus in a ticket's chat for member by,
	// once per key.
	Reply(ctx context.Context, id uuid.UUID, key string, in ReplyInput, by audit.Actor) (ConsoleMessage, error)
	// Resolve marks a ticket resolved; a carrier request's chat gets
	// "Đã ghi nhận hãng <name>.".
	Resolve(ctx context.Context, id uuid.UUID, by audit.Actor) (ConsoleTicket, error)
	// Reopen opens a resolved ticket again.
	Reopen(ctx context.Context, id uuid.UUID, by audit.Actor) (ConsoleTicket, error)
	// OpenCount counts every open ticket, for the console's drawer.
	OpenCount(ctx context.Context) (int, error)
}

// OpenInput is a ticket as its person asks for it.
type OpenInput struct {
	Kind        string     `json:"kind"`
	OrderID     *uuid.UUID `json:"order_id,omitempty"`
	CarrierName string     `json:"carrier_name,omitempty"`
	Body        string     `json:"body,omitempty"`
}

// ReplyInput is a member's reply.
type ReplyInput struct {
	Body    string
	ReplyTo *uuid.UUID
}

// TicketOrder is the TicketOrder schema.
type TicketOrder struct {
	ID           uuid.UUID `json:"id"`
	Title        string    `json:"title"`
	PhotoURL     string    `json:"photo_url,omitempty"`
	Carrier      string    `json:"carrier,omitempty"`
	TrackingCode string    `json:"tracking_code,omitempty"`
}

// TicketEvent is the TicketEvent schema.
type TicketEvent struct {
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`
}

// TicketView is the Ticket schema.
type TicketView struct {
	ID             uuid.UUID     `json:"id"`
	Kind           domain.Kind   `json:"kind"`
	Status         domain.Status `json:"status"`
	ConversationID uuid.UUID     `json:"conversation_id"`
	FirstMessageID *uuid.UUID    `json:"first_message_id,omitempty"`
	Order          *TicketOrder  `json:"order,omitempty"`
	CarrierName    string        `json:"carrier_name,omitempty"`
	OpenedAt       time.Time     `json:"opened_at"`
	Events         []TicketEvent `json:"events"`
}

// TicketListItem is the TicketListItem schema.
type TicketListItem struct {
	Ticket      TicketView           `json:"ticket"`
	LastMessage *chatsvc.MessageView `json:"last_message,omitempty"`
	Unread      int                  `json:"unread"`
}

// SummaryView is the SupportSummary schema.
type SummaryView struct {
	Open         int             `json:"open"`
	Unread       int             `json:"unread"`
	LatestUnread *TicketListItem `json:"latest_unread,omitempty"`
}

// ConsoleTicket is the ConsoleTicket schema.
type ConsoleTicket struct {
	Ticket        TicketView         `json:"ticket"`
	Person        chatsvc.PersonView `json:"person"`
	LastText      string             `json:"last_text,omitempty"`
	LastFrom      domain.From        `json:"last_from"`
	LastMessageAt time.Time          `json:"last_message_at"`
}

// ConsoleMessage is the ConsoleMessage schema.
type ConsoleMessage struct {
	Message   chatsvc.MessageView `json:"message"`
	Member    string              `json:"member,omitempty"`
	Automatic bool                `json:"automatic"`
}

// ConsoleTicketOrder is the ConsoleTicketOrder schema.
type ConsoleTicketOrder struct {
	ID             uuid.UUID          `json:"id"`
	Title          string             `json:"title"`
	Price          int64              `json:"price"`
	Status         string             `json:"status"`
	Seller         chatsvc.PersonView `json:"seller"`
	Carrier        string             `json:"carrier,omitempty"`
	TrackingCode   string             `json:"tracking_code,omitempty"`
	ShipmentStatus string             `json:"shipment_status,omitempty"`
	ShippedAt      *time.Time         `json:"shipped_at,omitempty"`
	DeliveredAt    *time.Time         `json:"delivered_at,omitempty"`
}

// ConsoleTicketDetail is the ConsoleTicketDetail schema.
type ConsoleTicketDetail struct {
	Ticket         ConsoleTicket             `json:"ticket"`
	PersonStatus   accountdomain.Status      `json:"person_status"`
	JoinedAt       time.Time                 `json:"joined_at"`
	Order          *ConsoleTicketOrder       `json:"order,omitempty"`
	SameCarrier30d *int                      `json:"same_carrier_30d,omitempty"`
	Others         []TicketView              `json:"others"`
	Messages       page.Page[ConsoleMessage] `json:"messages"`
}

// Deals is what support reads of module deal.
type Deals interface {
	Order(ctx context.Context, id uuid.UUID) (dealsvc.OrderFacts, bool, error)
}

// Listings is what support reads of module catalog.
type Listings interface {
	Summary(ctx context.Context, id uuid.UUID) (catalogsvc.Summary, bool, error)
}

// Deps is what New builds Support from.
type Deps struct {
	Tickets  repository.Tickets
	Accounts accountsvc.Service
	Chats    chatsvc.Service
	Deals    Deals
	Listings Listings
	// Ingress and Prefix reach this process's Restate services; chat's
	// MessageSend runs under the same prefix.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
}

// Support implements Service in this process.
type Support struct {
	tickets  repository.Tickets
	accounts accountsvc.Service
	chats    chatsvc.Service
	deals    Deals
	listings Listings
	prefix   string
	now      func() time.Time
	open     restatex.Caller[OpenFlowInput, OpenResult]
}

var _ Service = (*Support)(nil)

func New(d Deps) *Support {
	return &Support{
		tickets: d.Tickets, accounts: d.Accounts, chats: d.Chats, deals: d.Deals, listings: d.Listings,
		prefix: d.Prefix, now: d.Now,
		open: restatex.Caller[OpenFlowInput, OpenResult]{Ingress: d.Ingress, Service: d.Prefix + ticketOpen, Handler: "Run"},
	}
}

// Durable returns module support's Restate services, for Mount to bind.
func (s *Support) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(TicketOpen{Support: s})}
}

// view shows t; a report's order comes from deal and catalog.
func (s *Support) view(ctx context.Context, t repository.Ticket) (TicketView, error) {
	v := TicketView{
		ID: t.ID, Kind: t.Kind, Status: t.Status, ConversationID: t.ConversationID, FirstMessageID: t.FirstMessageID,
		CarrierName: t.CarrierName, OpenedAt: t.OpenedAt, Events: make([]TicketEvent, 0, len(t.Events)),
	}
	for _, e := range t.Events {
		v.Events = append(v.Events, TicketEvent{Kind: e.Kind, At: e.At})
	}
	if t.OrderID == nil {
		return v, nil
	}
	f, ok, err := s.deals.Order(ctx, *t.OrderID)
	if err != nil {
		return TicketView{}, fmt.Errorf("read the order: %w", err)
	}
	o := TicketOrder{ID: *t.OrderID}
	if ok {
		l, found, readErr := s.listings.Summary(ctx, f.ListingID)
		if readErr != nil {
			return TicketView{}, fmt.Errorf("read the listing: %w", readErr)
		}
		if found {
			o.Title, o.PhotoURL = l.Title, l.CoverThumbURL
		}
		if f.Shipment != nil {
			o.Carrier, o.TrackingCode = string(f.Shipment.Carrier), f.Shipment.TrackingCode
		}
	}
	v.Order = &o
	return v, nil
}

// person shows an account as chat shows the other member.
func person(id uuid.UUID, a accountsvc.AccountView) chatsvc.PersonView {
	return chatsvc.PersonView{
		ID: id, DisplayName: a.DisplayName, AvatarURL: a.AvatarURL, LastActiveAt: a.LastActiveAt,
		Deleted: a.Status == accountdomain.StatusDeleted, Official: id == accountdomain.ShopNexusID,
	}
}
