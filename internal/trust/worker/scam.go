// Package worker holds module trust's Restate handlers for the events it
// reacts to.
package worker

import (
	"fmt"
	"strings"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	chatdomain "github.com/shopnexus/server2/internal/chat/domain"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/trust/service"
)

// ScamWorker starts a scam check after each message with text and deletes
// a deleted account's warnings. Mount subscribes MessageSent to
// message.sent and AccountDeleted to account.deleted.
type ScamWorker struct {
	Prefix string
	Chats  chatsvc.Service
	Trust  service.Service
}

func (w ScamWorker) ServiceName() string { return w.Prefix + "TrustScamWorker" }

// MessageSent sends ScamCheck.Run for the message's conversation. The step
// returns the conversation ID only, never the text.
func (w ScamWorker) MessageSent(ctx restate.Context, e events.Event) error {
	conversation, err := restate.Run(ctx, func(rc restate.RunContext) (uuid.UUID, error) {
		f, found, stepErr := w.Chats.MessageFacts(rc, e.SubjectID)
		if stepErr != nil {
			return uuid.UUID{}, fmt.Errorf("read message: %w", stepErr)
		}
		// A support chat is with ShopNexus, which asks for no deposit.
		if !found || strings.TrimSpace(f.Text) == "" || f.Kind == chatdomain.KindSupport {
			return uuid.UUID{}, nil
		}
		return f.ConversationID, nil
	})
	if err != nil {
		return fmt.Errorf("read the message: %w", err)
	}
	if conversation == (uuid.UUID{}) {
		return nil
	}
	restate.ObjectSend(ctx, w.Prefix+service.ScamCheckName, conversation.String(), "Run").Send(restate.Void{})
	return nil
}

// AccountDeleted deletes the warnings of the account e names.
func (w ScamWorker) AccountDeleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Trust.DeleteScamWarnings(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s left: %w", e.SubjectID, err)
	}
	return nil
}
