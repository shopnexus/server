// Package worker holds module shipping's Restate services.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/shipping/service"
)

// AccountWatch drops a deleted account's carrier tokens: Mount subscribes
// Deleted to account.deleted.
type AccountWatch struct {
	Prefix   string
	Shipping service.Service
}

func (w AccountWatch) ServiceName() string { return w.Prefix + "ShippingAccountWatch" }

// Deleted drops the account's carrier accounts; a redelivered event finds
// nothing left to drop.
func (w AccountWatch) Deleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Shipping.AccountDeleted(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s deleted: %w", e.SubjectID, err)
	}
	return nil
}
