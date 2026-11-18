package worker

import (
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/events"
)

// AccountWatch ends a deleted account's deals: Mount subscribes Deleted to
// account.deleted. Deleted schedules Scrub domain.KeepRecipientsFor later,
// which blanks the recipients of the account's orders.
type AccountWatch struct {
	Prefix string
	Deals  service.Service
}

func (w AccountWatch) ServiceName() string { return w.Prefix + "DealAccountWatch" }

// Deleted ends the account's deals and schedules Scrub. Restate journals
// the send, so a retried invocation schedules one call; a redelivered event
// schedules a second, which blanks the same rows.
func (w AccountWatch) Deleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Deals.AccountDeleted(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s deleted: %w", e.SubjectID, err)
	}
	restate.ServiceSend(ctx, w.ServiceName(), "Scrub").Send(e.SubjectID, restate.WithDelay(domain.KeepRecipientsFor))
	return nil
}

// Scrub blanks the recipients of the deleted account's orders.
func (w AccountWatch) Scrub(ctx restate.Context, account uuid.UUID) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Deals.ScrubRecipients(rc, account)
	}); err != nil {
		return fmt.Errorf("scrub %s: %w", account, err)
	}
	return nil
}
