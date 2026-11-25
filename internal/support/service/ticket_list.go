package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/support/domain"
)

func (s *Support) Mine(ctx context.Context, account uuid.UUID) ([]TicketListItem, error) {
	ts, err := s.tickets.OfAccount(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("read tickets: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(ts))
	for i := range ts {
		ids = append(ids, ts[i].ConversationID)
	}
	threads, err := s.chats.Threads(ctx, account, ids)
	if err != nil {
		return nil, fmt.Errorf("read the chats: %w", err)
	}
	out := make([]TicketListItem, 0, len(ts))
	for i := range ts {
		v, viewErr := s.view(ctx, ts[i])
		if viewErr != nil {
			return nil, viewErr
		}
		th := threads[ts[i].ConversationID]
		item := TicketListItem{Ticket: v, LastMessage: th.Last, Unread: th.Unread}
		// The chat's first message is the ticket's card once the flow recorded it.
		if th.First != nil && ts[i].FirstMessageID != nil && th.First.ID == *ts[i].FirstMessageID {
			item.FirstMessage = th.First
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Support) Summary(ctx context.Context, account uuid.UUID) (SummaryView, error) {
	mine, err := s.Mine(ctx, account)
	if err != nil {
		return SummaryView{}, err
	}
	var out SummaryView
	for i := range mine {
		if mine[i].Ticket.Status == domain.StatusOpen {
			out.Open++
		}
		out.Unread += mine[i].Unread
		if mine[i].Unread == 0 || mine[i].LastMessage == nil {
			continue
		}
		if out.LatestUnread == nil || mine[i].LastMessage.CreatedAt.After(out.LatestUnread.LastMessage.CreatedAt) {
			out.LatestUnread = &mine[i]
		}
	}
	return out, nil
}

func (s *Support) OfOrder(ctx context.Context, account, order uuid.UUID) (TicketView, error) {
	t, err := s.tickets.ByOrder(ctx, account, order)
	if err != nil {
		return TicketView{}, fmt.Errorf("find the order's ticket: %w", err)
	}
	return s.view(ctx, t)
}

func (s *Support) OfConversation(ctx context.Context, account, conversation uuid.UUID) (TicketView, bool, error) {
	t, err := s.tickets.ByConversation(ctx, conversation)
	if errors.Is(err, domain.ErrNotFound) {
		return TicketView{}, false, nil
	}
	if err != nil {
		return TicketView{}, false, fmt.Errorf("find the chat's ticket: %w", err)
	}
	if t.AccountID != account {
		return TicketView{}, false, nil
	}
	v, err := s.view(ctx, t)
	return v, err == nil, err
}

func (s *Support) Messaged(ctx context.Context, conversation, message uuid.UUID, fromPerson bool, at time.Time) error {
	from := domain.FromShopNexus
	if fromPerson {
		from = domain.FromPerson
	}
	if _, err := s.tickets.Messaged(ctx, conversation, message, from, at); err != nil {
		return fmt.Errorf("record the message: %w", err)
	}
	return nil
}

func (s *Support) AccountDeleted(ctx context.Context, account, event uuid.UUID, at time.Time) error {
	if err := s.tickets.ResolveAllOf(ctx, account, event, at); err != nil {
		return fmt.Errorf("resolve the account's tickets: %w", err)
	}
	return nil
}
