// Package worker holds module assist's Restate handlers for the events it
// reacts to.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/assist/service"
	"github.com/shopnexus/server2/internal/platform/events"
)

// AccountWatch deletes a deleted account's reads and waiting transcripts: Mount subscribes
// Deleted to account.deleted. The account's frames are waiting uploads and
// expire within a day; a video still held is consumed when its run ends.
type AccountWatch struct {
	Prefix string
	Reads  service.Service
}

func (w AccountWatch) ServiceName() string { return w.Prefix + "AssistAccountWatch" }

// Deleted deletes the reads of the account e names.
func (w AccountWatch) Deleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Reads.DeleteAccount(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s left: %w", e.SubjectID, err)
	}
	return nil
}
