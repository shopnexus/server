package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/support/domain"
	"github.com/shopnexus/server/internal/support/repository"
)

// consoleTickets shows tickets with their persons and their chats' last
// lines, read in one query each.
func (s *Support) consoleTickets(ctx context.Context, ts []repository.Ticket) ([]ConsoleTicket, error) {
	people := make([]uuid.UUID, 0, len(ts))
	convs := make([]uuid.UUID, 0, len(ts))
	for i := range ts {
		people, convs = append(people, ts[i].AccountID), append(convs, ts[i].ConversationID)
	}
	accounts, err := s.d.Accounts.GetMany(ctx, people)
	if err != nil {
		return nil, fmt.Errorf("read the persons: %w", err)
	}
	contacts, err := s.d.Accounts.Contacts(ctx, people)
	if err != nil {
		return nil, fmt.Errorf("read the persons' contacts: %w", err)
	}
	threads, err := s.d.Chats.Threads(ctx, accountdomain.ShopNexusID, convs)
	if err != nil {
		return nil, fmt.Errorf("read the chats: %w", err)
	}
	out := make([]ConsoleTicket, 0, len(ts))
	for i := range ts {
		v, viewErr := s.view(ctx, ts[i])
		if viewErr != nil {
			return nil, viewErr
		}
		c := ConsoleTicket{
			Ticket: v, Person: chatsvc.PersonFromAccount(ts[i].AccountID, accounts[ts[i].AccountID]), PersonContact: contacts[ts[i].AccountID],
			LastFrom: ts[i].LastFrom, LastMessageAt: ts[i].LastMessageAt,
		}
		if last := threads[ts[i].ConversationID].Last; last != nil {
			c.LastText = last.Text
		}
		out = append(out, c)
	}
	return out, nil
}

// Queue is UC-MOD-09 step 3.
func (s *Support) Queue(ctx context.Context, status domain.Status, kind *domain.Kind, p page.Request) (page.Page[ConsoleTicket], error) {
	ts, err := s.d.Tickets.Queue(ctx, status, kind, p)
	if err != nil {
		return page.Page[ConsoleTicket]{}, fmt.Errorf("read the queue: %w", err)
	}
	items, err := s.consoleTickets(ctx, ts)
	if err != nil {
		return page.Page[ConsoleTicket]{}, err
	}
	return page.Of(items, p, func(c ConsoleTicket) uuid.UUID { return c.Ticket.ID }), nil
}

// Detail is UC-MOD-09 step 3.
func (s *Support) Detail(ctx context.Context, id uuid.UUID) (ConsoleTicketDetail, error) {
	t, err := s.d.Tickets.Get(ctx, id)
	if err != nil {
		return ConsoleTicketDetail{}, fmt.Errorf("read the ticket: %w", err)
	}
	shown, err := s.consoleTickets(ctx, []repository.Ticket{t})
	if err != nil {
		return ConsoleTicketDetail{}, err
	}
	who, err := s.d.Accounts.GetMany(ctx, []uuid.UUID{t.AccountID})
	if err != nil {
		return ConsoleTicketDetail{}, fmt.Errorf("read the person: %w", err)
	}
	out := ConsoleTicketDetail{
		Ticket: shown[0], PersonStatus: who[t.AccountID].Status, JoinedAt: who[t.AccountID].CreatedAt, Others: []TicketView{},
	}
	if t.OrderID != nil {
		f, ok, readErr := s.d.Deals.Order(ctx, *t.OrderID)
		if readErr != nil {
			return ConsoleTicketDetail{}, fmt.Errorf("read the order: %w", readErr)
		}
		if ok {
			sellers, sellerErr := s.d.Accounts.GetMany(ctx, []uuid.UUID{f.SellerID})
			if sellerErr != nil {
				return ConsoleTicketDetail{}, fmt.Errorf("read the seller: %w", sellerErr)
			}
			o := &ConsoleTicketOrder{
				ID: *t.OrderID, Title: shown[0].Ticket.Order.Title, Price: f.Price, Status: string(f.Status),
				Seller: chatsvc.PersonFromAccount(f.SellerID, sellers[f.SellerID]), ShippedAt: f.ShippedAt,
			}
			if sh := f.Shipment; sh != nil {
				o.Carrier, o.TrackingCode, o.ShipmentStatus, o.DeliveredAt = string(sh.Carrier), sh.TrackingCode, string(sh.Status), sh.DeliveredAt
			}
			out.Order = o
		}
	}
	if t.Kind == domain.KindCarrierRequest {
		n, countErr := s.d.Tickets.SameCarrier(ctx, domain.CarrierKey(t.CarrierName), s.d.Now().Add(-domain.SameCarrierWindow))
		if countErr != nil {
			return ConsoleTicketDetail{}, fmt.Errorf("count requests for the carrier: %w", countErr)
		}
		out.SameCarrier30d = &n
	}
	others, err := s.d.Tickets.OfAccount(ctx, t.AccountID)
	if err != nil {
		return ConsoleTicketDetail{}, fmt.Errorf("read the person's tickets: %w", err)
	}
	for i := range others {
		if others[i].ID == t.ID {
			continue
		}
		v, viewErr := s.view(ctx, others[i])
		if viewErr != nil {
			return ConsoleTicketDetail{}, viewErr
		}
		out.Others = append(out.Others, v)
	}
	if out.Messages, err = s.Messages(ctx, id, page.Request{}); err != nil {
		return ConsoleTicketDetail{}, err
	}
	// A member opening the ticket reads its chat for ShopNexus, so the person sees "Đã xem".
	if newest := out.Messages.Items; len(newest) > 0 && newest[0].Message.Status == chatdomain.StatusSent {
		if err = s.d.Chats.Read(ctx, accountdomain.ShopNexusID, t.ConversationID, newest[0].Message.ID); err != nil {
			return ConsoleTicketDetail{}, fmt.Errorf("mark the chat read: %w", err)
		}
	}
	return out, nil
}

func (s *Support) Messages(ctx context.Context, id uuid.UUID, p page.Request) (page.Page[ConsoleMessage], error) {
	t, err := s.d.Tickets.Get(ctx, id)
	if err != nil {
		return page.Page[ConsoleMessage]{}, fmt.Errorf("read the ticket: %w", err)
	}
	msgs, err := s.d.Chats.Messages(ctx, accountdomain.ShopNexusID, t.ConversationID, p)
	if err != nil {
		return page.Page[ConsoleMessage]{}, fmt.Errorf("read the chat: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(msgs.Items))
	for i := range msgs.Items {
		ids = append(ids, msgs.Items[i].ID)
	}
	replies, err := s.d.Tickets.Members(ctx, ids)
	if err != nil {
		return page.Page[ConsoleMessage]{}, fmt.Errorf("read who replied: %w", err)
	}
	out := page.Page[ConsoleMessage]{Items: make([]ConsoleMessage, 0, len(msgs.Items)), NextCursor: msgs.NextCursor}
	for i := range msgs.Items {
		r := replies[msgs.Items[i].ID]
		out.Items = append(out.Items, ConsoleMessage{Message: msgs.Items[i], Member: r.Member, Automatic: r.Automatic})
	}
	return out, nil
}

// Reply is UC-MOD-09 step 4.
func (s *Support) Reply(ctx context.Context, id, key uuid.UUID, in ReplyInput, by audit.Actor) (ConsoleMessage, error) {
	t, err := s.d.Tickets.Get(ctx, id)
	if err != nil {
		return ConsoleMessage{}, fmt.Errorf("read the ticket: %w", err)
	}
	m, err := s.d.Chats.Send(ctx, accountdomain.ShopNexusID, t.ConversationID, key, chatsvc.SendInput{Text: in.Body, ReplyTo: in.ReplyTo})
	if err != nil {
		return ConsoleMessage{}, fmt.Errorf("send the reply: %w", err)
	}
	if err = s.d.Tickets.RecordReply(ctx, t.ID, m.ID, &by, s.d.Now()); err != nil {
		return ConsoleMessage{}, fmt.Errorf("record the reply: %w", err)
	}
	replies, err := s.d.Tickets.Members(ctx, []uuid.UUID{m.ID})
	if err != nil {
		return ConsoleMessage{}, fmt.Errorf("read who replied: %w", err)
	}
	return ConsoleMessage{Message: m, Member: replies[m.ID].Member}, nil
}

// Resolve is UC-MOD-09 step 5.
func (s *Support) Resolve(ctx context.Context, id uuid.UUID, by audit.Actor) (ConsoleTicket, error) {
	t, err := s.d.Tickets.Get(ctx, id)
	if err != nil {
		return ConsoleTicket{}, fmt.Errorf("read the ticket: %w", err)
	}
	// A3
	if t.Kind == domain.KindCarrierRequest && t.Status == domain.StatusOpen {
		// The note goes out before the status moves, so a failed note is
		// sent again when the member resolves again. The key names this
		// resolving: the retry sends it once, and a ticket reopened and
		// resolved again sends it again.
		key := restatex.DerivedID("resolve", t.ID.String(), strconv.Itoa(len(t.Events)+1))
		m, sendErr := s.d.Chats.Send(ctx, accountdomain.ShopNexusID, t.ConversationID, key,
			chatsvc.SendInput{Text: "Đã ghi nhận hãng " + t.CarrierName + "."})
		switch {
		case errors.Is(sendErr, accountdomain.ErrAccountDeleted):
			// Nobody is left to read it; the ticket still resolves.
		case sendErr != nil:
			return ConsoleTicket{}, fmt.Errorf("send the note: %w", sendErr)
		default:
			if err = s.d.Tickets.RecordReply(ctx, t.ID, m.ID, nil, s.d.Now()); err != nil {
				return ConsoleTicket{}, fmt.Errorf("record the note: %w", err)
			}
		}
	}
	if t, err = s.d.Tickets.Resolve(ctx, id, uuid.NewV7(), &by, s.d.Now()); err != nil {
		return ConsoleTicket{}, fmt.Errorf("resolve the ticket: %w", err)
	}
	shown, err := s.consoleTickets(ctx, []repository.Ticket{t})
	if err != nil {
		return ConsoleTicket{}, err
	}
	return shown[0], nil
}

// Reopen is UC-MOD-09 A2.
func (s *Support) Reopen(ctx context.Context, id uuid.UUID, by audit.Actor) (ConsoleTicket, error) {
	t, err := s.d.Tickets.Reopen(ctx, id, uuid.NewV7(), &by, s.d.Now())
	if err != nil {
		return ConsoleTicket{}, fmt.Errorf("reopen the ticket: %w", err)
	}
	shown, err := s.consoleTickets(ctx, []repository.Ticket{t})
	if err != nil {
		return ConsoleTicket{}, err
	}
	return shown[0], nil
}

// TicketsOfLimit is how many tickets TicketsOf lists.
const TicketsOfLimit = 50

func (s *Support) TicketsOf(ctx context.Context, account uuid.UUID) ([]ConsoleTicket, error) {
	ts, err := s.d.Tickets.Involving(ctx, account, TicketsOfLimit)
	if err != nil {
		return nil, fmt.Errorf("read the account's tickets: %w", err)
	}
	return s.consoleTickets(ctx, ts)
}

func (s *Support) OpenCount(ctx context.Context) (int, error) {
	n, err := s.d.Tickets.CountOpen(ctx)
	if err != nil {
		return 0, fmt.Errorf("count open tickets: %w", err)
	}
	return n, nil
}

func (s *Support) CountByStatus(ctx context.Context, since time.Time) (map[domain.Status]int, error) {
	n, err := s.d.Tickets.CountByStatus(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("count tickets: %w", err)
	}
	return n, nil
}
