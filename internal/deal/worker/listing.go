// Package worker holds module deal's Restate handlers for the events it
// reacts to.
package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/events"
)

// ListingWatch keeps deals in line with their listings: Mount subscribes
// Changed to listing.updated, listing.hidden, listing.sold and
// listing.deleted.
type ListingWatch struct {
	Prefix string
	Deals  service.Service
}

func (w ListingWatch) ServiceName() string { return w.Prefix + "DealListingWatch" }

// Changed reads the listing of e and brings its deals in line with it.
func (w ListingWatch) Changed(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Deals.ListingChanged(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("listing %s changed: %w", e.SubjectID, err)
	}
	return nil
}
