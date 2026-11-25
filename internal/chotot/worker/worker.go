// Package worker holds module chotot's Restate handlers for the events it
// reacts to.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/chotot/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// ListingWatch keeps the imports in line with their copies and accounts:
// Mount subscribes each handler to its event.
type ListingWatch struct {
	Prefix string
	Chotot service.Service
}

func (w ListingWatch) ServiceName() string { return w.Prefix + "ChototListingWatch" }

// Sold reminds the seller to hide the listing on Chợ Tốt, on listing.sold.
// The reminder's event ID derives from e's, so a redelivery writes it once.
func (w ListingWatch) Sold(ctx restate.Context, e events.Event) error {
	ev := restatex.DerivedID("chotot_import.sold", e.ID.String())
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Chotot.ListingSold(rc, e.SubjectID, ev, e.OccurredAt)
	}); err != nil {
		return fmt.Errorf("listing %s sold: %w", e.SubjectID, err)
	}
	return nil
}

// Updated clears the review of the listing's import, on listing.updated.
func (w ListingWatch) Updated(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Chotot.ListingEdited(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("listing %s updated: %w", e.SubjectID, err)
	}
	return nil
}

// Deleted forgets the listing's import, on listing.deleted.
func (w ListingWatch) Deleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Chotot.ListingDeleted(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("listing %s deleted: %w", e.SubjectID, err)
	}
	return nil
}

// AccountDeleted removes the account's code, link and imports, on
// account.deleted.
func (w ListingWatch) AccountDeleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Chotot.DeleteAccount(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("account %s left: %w", e.SubjectID, err)
	}
	return nil
}
