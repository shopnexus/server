package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/events"
)

// ChatPost writes deal messages: Mount subscribes Moved to deal.moved.
type ChatPost struct {
	Prefix string
	Deals  service.Service
}

func (w ChatPost) ServiceName() string { return w.Prefix + "DealChatPost" }

// Moved posts the moves e carries into the deal's conversation. Each
// message takes its move's ID, so a retried step or a redelivered event
// writes no second one.
func (w ChatPost) Moved(ctx restate.Context, e events.Event) error {
	ids := service.MovedIDs(e)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Deals.PostMoves(rc, e.SubjectID, ids)
	}); err != nil {
		return fmt.Errorf("deal %s moved: %w", e.SubjectID, err)
	}
	return nil
}
