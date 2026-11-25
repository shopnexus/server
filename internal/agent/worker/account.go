// Package worker holds module agent's Restate handlers for the events it
// reacts to.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/events"
)

// AccountWatch blanks the tool outputs of a deleted account's
// conversation: Mount subscribes Deleted to account.deleted. The turns and
// the read record stay, as the console's actions do.
type AccountWatch struct {
	Prefix string
	Agent  service.Service
}

func (w AccountWatch) ServiceName() string { return w.Prefix + "AgentAccountWatch" }

// Deleted blanks the tool outputs about the account e names.
func (w AccountWatch) Deleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Agent.AccountDeleted(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s left: %w", e.SubjectID, err)
	}
	return nil
}
