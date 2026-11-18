// Package worker holds module discovery's Restate handlers for the events
// it reacts to.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/events"
)

// AccountWatch deletes a deleted account's saves and search history: Mount
// subscribes Deleted to account.deleted.
type AccountWatch struct {
	Prefix    string
	Discovery service.Service
}

func (w AccountWatch) ServiceName() string { return w.Prefix + "DiscoveryAccountWatch" }

// Deleted clears what discovery keeps of the account e names.
func (w AccountWatch) Deleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Discovery.AccountDeleted(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s deleted: %w", e.SubjectID, err)
	}
	return nil
}
