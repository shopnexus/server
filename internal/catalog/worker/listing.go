package worker

import (
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/events"
)

// ListingWatch queues a listing whose page changed for IndexNow: Mount
// subscribes Changed to the listing events a search engine would see, when
// IndexNow is on.
type ListingWatch struct {
	Prefix string
}

func (w ListingWatch) ServiceName() string { return w.Prefix + "CatalogListingWatch" }

// Changed sends the listing e names to the IndexNow queue.
func (w ListingWatch) Changed(ctx restate.Context, e events.Event) error {
	restate.ObjectSend(ctx, w.Prefix+service.IndexNowName, service.IndexNowQueue, "Add").Send(e.SubjectID)
	return nil
}
