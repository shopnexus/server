// Package worker holds module feedback's Restate handler for the events it
// follows.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/feedback/service"
	"github.com/shopnexus/server/internal/platform/events"
)

// Worker follows accounts: Mount subscribes AccountDeleted to
// account.deleted.
type Worker struct {
	Prefix   string
	Feedback service.Service
}

func (w Worker) ServiceName() string { return w.Prefix + "FeedbackWorker" }

// AccountDeleted deletes the account's feedback, so no text of it stays.
func (w Worker) AccountDeleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Feedback.AccountDeleted(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s deleted: %w", e.SubjectID, err)
	}
	return nil
}
