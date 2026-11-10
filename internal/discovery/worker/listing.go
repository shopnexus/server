package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/platform/events"
)

// ListingWatch records first publications for the search watches and
// weighs the saves of a listing whose price may have moved: Mount
// subscribes Published to listing.published and Updated to listing.updated.
type ListingWatch struct {
	Prefix    string
	Discovery service.Service
}

func (w ListingWatch) ServiceName() string { return w.Prefix + "DiscoveryListingWatch" }

// Published records a first publication, then weighs the saves: a listing
// back from processing after an edit may carry a new price.
func (w ListingWatch) Published(ctx restate.Context, e events.Event) error {
	if first, _ := e.Payload["first"].(bool); first { //nolint:revive // an event without the flag is not a first publication
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
			return w.Discovery.ListingArrived(rc, e.SubjectID)
		}); err != nil {
			return fmt.Errorf("listing %s arrived: %w", e.SubjectID, err)
		}
	}
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Discovery.PriceMoved(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("listing %s published: %w", e.SubjectID, err)
	}
	return nil
}

// Updated weighs the saves of an edited listing at its current price.
func (w ListingWatch) Updated(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Discovery.PriceMoved(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("listing %s updated: %w", e.SubjectID, err)
	}
	return nil
}
