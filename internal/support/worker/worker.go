// Package worker holds module support's Restate handlers for the events it
// follows.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/support/service"
)

// Worker follows chat and accounts: Mount subscribes MessageSent to
// message.sent and AccountDeleted to account.deleted.
type Worker struct {
	Prefix  string
	Chats   chatsvc.Service
	Support service.Service
}

func (w Worker) ServiceName() string { return w.Prefix + "SupportWorker" }

// MessageSent moves the ticket of a support chat's message: the person's
// message reopens a resolved ticket and asks for an answer, and ShopNexus's
// marks it answered. A pair chat's message changes nothing.
func (w Worker) MessageSent(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		f, found, err := w.Chats.MessageFacts(rc, e.SubjectID)
		if err != nil {
			return fmt.Errorf("read the message: %w", err)
		}
		if !found || f.Kind != chatdomain.KindSupport {
			return nil
		}
		if err = w.Support.Messaged(rc, f.ConversationID, e.SubjectID, f.RecipientID == accountdomain.ShopNexusID, e.OccurredAt); err != nil {
			return fmt.Errorf("record the message: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("message %s: %w", e.SubjectID, err)
	}
	return nil
}

// AccountDeleted resolves the account's open tickets.
func (w Worker) AccountDeleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		if err := w.Support.AccountDeleted(rc, e.SubjectID, e.OccurredAt); err != nil {
			return fmt.Errorf("resolve tickets: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("account %s deleted: %w", e.SubjectID, err)
	}
	return nil
}
