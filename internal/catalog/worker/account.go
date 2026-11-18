// Package worker holds module catalog's Restate handlers for the events it
// reacts to.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/events"
)

// AccountWatch deletes a deleted seller's listings and drafts: Mount
// subscribes Deleted to account.deleted.
type AccountWatch struct {
	Prefix   string
	Listings service.Service
}

func (w AccountWatch) ServiceName() string { return w.Prefix + "CatalogAccountWatch" }

// Deleted deletes the listings of the account e names.
func (w AccountWatch) Deleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Listings.SellerLeft(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("seller %s left: %w", e.SubjectID, err)
	}
	return nil
}
