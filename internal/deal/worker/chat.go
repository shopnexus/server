package worker

import (
	"errors"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	chatdomain "github.com/shopnexus/server2/internal/chat/domain"
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
// writes no second one. A move chat refuses as misplaced ends the
// invocation, since no retry would place it.
func (w ChatPost) Moved(ctx restate.Context, e events.Event) error {
	ids := service.MovedIDs(e)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		err := w.Deals.PostMoves(rc, e.SubjectID, ids)
		if errors.Is(err, chatdomain.ErrMisplacedMove) {
			return restate.ToTerminalError(err)
		}
		if err != nil {
			return fmt.Errorf("post the moves: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("deal %s moved: %w", e.SubjectID, err)
	}
	return nil
}
