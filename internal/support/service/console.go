package service

import (
	"context"
	"fmt"
	"strconv"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
)

// consoleTickets shows tickets with their persons and their chats' last
// lines, read in one query each.
func (s *Support) consoleTickets(ctx context.Context, ts []repository.Ticket) ([]ConsoleTicket, error) {
	people := make([]uuid.UUID, 0, len(ts))
	convs := make([]uuid.UUID, 0, len(ts))
	for i := range ts {
		people, convs = append(people, ts[i].AccountID), append(convs, ts[i].ConversationID)
	}
	accounts, err := s.accounts.GetMany(ctx, people)
	if err != nil {
		return nil, fmt.Errorf("read the persons: %w", err)
	}
	threads, err := s.chats.Threads(ctx, accountdomain.ShopNexusID, convs)
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
			Ticket: v, Person: person(ts[i].AccountID, accounts[ts[i].AccountID]), LastFrom: ts[i].LastFrom,
			LastMessageAt: ts[i].LastMessageAt,
		}
		if last := threads[ts[i].ConversationID].Last; last != nil {
			c.LastText = last.Text
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Support) Queue(ctx context.Context, status domain.Status, kind *domain.Kind, p page.Request) (page.Page[ConsoleTicket], error) {
	ts, err := s.tickets.Queue(ctx, status, kind, p)
	if err != nil {
		return page.Page[ConsoleTicket]{}, fmt.Errorf("read the queue: %w", err)
	}
	items, err := s.consoleTickets(ctx, ts)
	if err != nil {
		return page.Page[ConsoleTicket]{}, err
	}
	return page.Of(items, p, func(c ConsoleTicket) uuid.UUID { return c.Ticket.ID }), nil
}

func (s *Support) Detail(ctx context.Context, id uuid.UUID) (ConsoleTicketDetail, error) {
	t, err := s.tickets.Get(ctx, id)
	if err != nil {
		return ConsoleTicketDetail{}, fmt.Errorf("read the ticket: %w", err)
	}
	shown, err := s.consoleTickets(ctx, []repository.Ticket{t})
	if err != nil {
		return ConsoleTicketDetail{}, err
	}
	who, err := s.accounts.GetMany(ctx, []uuid.UUID{t.AccountID})
	if err != nil {
		return ConsoleTicketDetail{}, fmt.Errorf("read the person: %w", err)
	}
	out := ConsoleTicketDetail{
		Ticket: shown[0], PersonStatus: who[t.AccountID].Status, JoinedAt: who[t.AccountID].CreatedAt, Others: []TicketView{},
	}
	if t.OrderID != nil {
		f, ok, readErr := s.deals.Order(ctx, *t.OrderID)
		if readErr != nil {
			return ConsoleTicketDetail{}, fmt.Errorf("read the order: %w", readErr)
		}
		if ok {
			sellers, sellerErr := s.accounts.GetMany(ctx, []uuid.UUID{f.SellerID})
			if sellerErr != nil {
				return ConsoleTicketDetail{}, fmt.Errorf("read the seller: %w", sellerErr)
			}
			o := &ConsoleTicketOrder{
				ID: *t.OrderID, Title: shown[0].Ticket.Order.Title, Price: f.Price, Status: string(f.Status),
				Seller: person(f.SellerID, sellers[f.SellerID]), ShippedAt: f.ShippedAt,
			}
			if sh := f.Shipment; sh != nil {
				o.Carrier, o.TrackingCode, o.ShipmentStatus, o.DeliveredAt = string(sh.Carrier), sh.TrackingCode, string(sh.Status), sh.DeliveredAt
			}
			out.Order = o
		}
	}
	if t.Kind == domain.KindCarrierRequest {
		n, countErr := s.tickets.SameCarrier(ctx, domain.CarrierKey(t.CarrierName), s.now().Add(-domain.SameCarrierWindow))
		if countErr != nil {
			return ConsoleTicketDetail{}, fmt.Errorf("count requests for the carrier: %w", countErr)
		}
		out.SameCarrier30d = &n
	}
	others, err := s.tickets.OfAccount(ctx, t.AccountID)
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
	return out, nil
}

func (s *Support) Messages(ctx context.Context, id uuid.UUID, p page.Request) (page.Page[ConsoleMessage], error) {
	t, err := s.tickets.Get(ctx, id)
	if err != nil {
		return page.Page[ConsoleMessage]{}, fmt.Errorf("read the ticket: %w", err)
	}
	msgs, err := s.chats.Messages(ctx, accountdomain.ShopNexusID, t.ConversationID, p)
	if err != nil {
		return page.Page[ConsoleMessage]{}, fmt.Errorf("read the chat: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(msgs.Items))
	for i := range msgs.Items {
		ids = append(ids, msgs.Items[i].ID)
	}
	replies, err := s.tickets.Members(ctx, ids)
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

func (s *Support) Reply(ctx context.Context, id uuid.UUID, key string, in ReplyInput, by audit.Actor) (ConsoleMessage, error) {
	t, err := s.tickets.Get(ctx, id)
	if err != nil {
		return ConsoleMessage{}, fmt.Errorf("read the ticket: %w", err)
	}
	m, err := s.chats.Send(ctx, accountdomain.ShopNexusID, t.ConversationID, "reply/"+key, chatsvc.SendInput{Text: in.Body, ReplyTo: in.ReplyTo})
	if err != nil {
		return ConsoleMessage{}, fmt.Errorf("send the reply: %w", err)
	}
	if err = s.tickets.RecordReply(ctx, t.ID, m.ID, &by, s.now()); err != nil {
		return ConsoleMessage{}, fmt.Errorf("record the reply: %w", err)
	}
	replies, err := s.tickets.Members(ctx, []uuid.UUID{m.ID})
	if err != nil {
		return ConsoleMessage{}, fmt.Errorf("read who replied: %w", err)
	}
	return ConsoleMessage{Message: m, Member: replies[m.ID].Member}, nil
}

func (s *Support) Resolve(ctx context.Context, id uuid.UUID, by audit.Actor) (ConsoleTicket, error) {
	t, err := s.tickets.Resolve(ctx, id, &by, s.now())
	if err != nil {
		return ConsoleTicket{}, fmt.Errorf("resolve the ticket: %w", err)
	}
	if t.Kind == domain.KindCarrierRequest {
		// The key names this resolving, so a ticket reopened and resolved
		// again sends the note again.
		key := "resolve/" + t.ID.String() + "/" + strconv.Itoa(len(t.Events))
		m, sendErr := s.chats.Send(ctx, accountdomain.ShopNexusID, t.ConversationID, key,
			chatsvc.SendInput{Text: "Đã ghi nhận hãng " + t.CarrierName + "."})
		if sendErr != nil {
			return ConsoleTicket{}, fmt.Errorf("send the note: %w", sendErr)
		}
		if err = s.tickets.RecordReply(ctx, t.ID, m.ID, nil, s.now()); err != nil {
			return ConsoleTicket{}, fmt.Errorf("record the note: %w", err)
		}
	}
	shown, err := s.consoleTickets(ctx, []repository.Ticket{t})
	if err != nil {
		return ConsoleTicket{}, err
	}
	return shown[0], nil
}

func (s *Support) Reopen(ctx context.Context, id uuid.UUID, by audit.Actor) (ConsoleTicket, error) {
	t, err := s.tickets.Reopen(ctx, id, &by, s.now())
	if err != nil {
		return ConsoleTicket{}, fmt.Errorf("reopen the ticket: %w", err)
	}
	shown, err := s.consoleTickets(ctx, []repository.Ticket{t})
	if err != nil {
		return ConsoleTicket{}, err
	}
	return shown[0], nil
}

func (s *Support) OpenCount(ctx context.Context) (int, error) {
	n, err := s.tickets.CountOpen(ctx)
	if err != nil {
		return 0, fmt.Errorf("count open tickets: %w", err)
	}
	return n, nil
}
