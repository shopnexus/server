package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
)

// ticketOpen is the TicketOpen service's name without the prefix.
const ticketOpen = "TicketOpen"

// OpenFlowInput is one request for a ticket.
type OpenFlowInput struct {
	Account uuid.UUID `json:"account"`
	Key     uuid.UUID `json:"key"`
	Input   OpenInput `json:"input"`
}

// OpenResult names the ticket opened or found, or carries the refusal.
type OpenResult struct {
	TicketID uuid.UUID     `json:"ticket_id"`
	Existed  bool          `json:"existed"`
	Refused  *apperr.Error `json:"refused,omitempty"`
}

func (r OpenResult) Refusal() *apperr.Error { return r.Refused }

// openFacts is what the read step found for a request.
type openFacts struct {
	Refused *apperr.Error `json:"refused,omitempty"`
	// Existing is the order's ticket, for a second report of one order.
	Existing *uuid.UUID `json:"existing,omitempty"`
}

// inserted is the insert step's outcome.
type inserted struct {
	TicketID uuid.UUID     `json:"ticket_id"`
	Existed  bool          `json:"existed"`
	Refused  *apperr.Error `json:"refused,omitempty"`
}

// TicketOpen opens a ticket: one step reads whether the account may and
// whether the order has its ticket; then the support chat opens, the
// ticket goes in under the account's daily limit, chat's MessageSend sends
// the person's first message, and the ticket records it as its card. The
// facts step refuses a request past the limit; one that passes it in a race
// and is refused at the insert leaves an empty support chat, which no list
// shows.
type TicketOpen struct{ Support *Support }

func (o TicketOpen) ServiceName() string { return o.Support.prefix + ticketOpen }

func (o TicketOpen) Run(ctx restate.Context, in OpenFlowInput) (OpenResult, error) {
	s := o.Support
	draft, err := domain.NewTicket(in.Input.Kind, in.Input.CarrierName, in.Input.Body, in.Input.OrderID)
	if err != nil {
		return OpenResult{Refused: apperr.From(err)}, nil
	}
	at, err := restatex.Now(ctx, s.now)
	if err != nil {
		return OpenResult{}, err
	}
	ticketID, conversationID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	f, err := restate.Run(ctx, func(rc restate.RunContext) (openFacts, error) {
		status, stepErr := s.accounts.Status(rc, in.Account)
		if stepErr != nil {
			return openFacts{}, fmt.Errorf("read account status: %w", stepErr)
		}
		// A suspended person may still ask why.
		if r := status.Refusal(); r != nil && status != accountdomain.StatusSuspended {
			return openFacts{Refused: r}, nil
		}
		if draft.Kind == domain.KindNotReceived {
			existing, getErr := s.tickets.ByOrder(rc, in.Account, *draft.OrderID)
			if getErr == nil {
				return openFacts{Existing: &existing.ID}, nil
			}
			if !errors.Is(getErr, domain.ErrNotFound) {
				return openFacts{}, fmt.Errorf("find the order's ticket: %w", getErr)
			}
		}
		// Refused here, a request past the limit opens no chat; the insert
		// counts again under the account's lock.
		n, countErr := s.tickets.OpenedSince(rc, in.Account, at.Add(-24*time.Hour))
		if countErr != nil {
			return openFacts{}, fmt.Errorf("count the day's tickets: %w", countErr)
		}
		if n >= domain.DailyLimit {
			return openFacts{Refused: domain.ErrTooMany}, nil
		}
		if draft.Kind != domain.KindNotReceived {
			return openFacts{}, nil
		}
		order, ok, readErr := s.deals.Order(rc, *draft.OrderID)
		if readErr != nil {
			return openFacts{}, fmt.Errorf("read the order: %w", readErr)
		}
		if !ok {
			return openFacts{Refused: domain.ErrOrderNotFound}, nil
		}
		check := domain.OrderCheck{
			BuyerID: order.BuyerID, Status: string(order.Status), CompletedBy: string(order.CompletedBy), ClosedAt: order.ClosedAt,
		}
		if mayErr := domain.MayReport(check, in.Account, at); mayErr != nil {
			return openFacts{Refused: apperr.From(mayErr)}, nil
		}
		return openFacts{}, nil
	})
	if err != nil {
		return OpenResult{}, fmt.Errorf("read the request's facts: %w", err)
	}
	if f.Refused != nil {
		return OpenResult{Refused: f.Refused}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	if f.Existing != nil {
		return OpenResult{TicketID: *f.Existing, Existed: true}, nil
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		if _, openErr := s.chats.OpenSupport(rc, in.Account, conversationID); openErr != nil {
			return fmt.Errorf("open: %w", openErr)
		}
		return nil
	}); err != nil {
		return OpenResult{}, fmt.Errorf("open the support chat: %w", err)
	}
	ins, err := restate.Run(ctx, func(rc restate.RunContext) (inserted, error) {
		t, existed, insErr := s.tickets.Insert(rc, repository.Ticket{
			ID: ticketID, AccountID: in.Account, ConversationID: conversationID, Kind: draft.Kind, OrderID: draft.OrderID,
			CarrierName: draft.CarrierName, OpenedAt: at,
		}, in.Key, draft.CarrierKey, at.Add(-24*time.Hour))
		if errors.Is(insErr, domain.ErrTooMany) {
			return inserted{Refused: domain.ErrTooMany}, nil
		}
		if insErr != nil {
			return inserted{}, fmt.Errorf("insert the ticket: %w", insErr)
		}
		return inserted{TicketID: t.ID, Existed: existed}, nil
	})
	if err != nil {
		return OpenResult{}, fmt.Errorf("insert the ticket: %w", err)
	}
	if ins.Refused != nil || ins.Existed {
		return OpenResult(ins), nil //nolint:nilerr // As above.
	}
	sent, err := restate.Service[chatsvc.SendResult](ctx, s.prefix+chatsvc.MessageSendName, "Run").
		Request(chatsvc.SendFlowInput{Sender: in.Account, ConversationID: conversationID, Text: draft.Body, OpensTicket: true})
	if err != nil {
		return OpenResult{}, fmt.Errorf("send the first message: %w", err)
	}
	if sent.Refused != nil {
		return OpenResult{Refused: sent.Refused}, nil //nolint:nilerr // As above.
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return s.tickets.SetFirstMessage(rc, ticketID, sent.MessageID)
	}); err != nil {
		return OpenResult{}, fmt.Errorf("record the first message: %w", err)
	}
	return OpenResult{TicketID: ticketID}, nil
}
