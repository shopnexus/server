// Package worker holds module chat's Restate handlers for the events it
// reacts to.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/events"
)

// AccountWatch deletes a deleted account's quick replies and reply samples:
// Mount subscribes Deleted to account.deleted. Its messages stay, as the
// other member's conversation does.
type AccountWatch struct {
	Prefix string
	Chats  service.Service
}

func (w AccountWatch) ServiceName() string { return w.Prefix + "ChatAccountWatch" }

// Deleted deletes the quick replies and reply samples of the account e
// names.
func (w AccountWatch) Deleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Chats.DeleteAccount(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s left: %w", e.SubjectID, err)
	}
	return nil
}
