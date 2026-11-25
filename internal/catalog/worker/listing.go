package worker

import (
	"context"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/catalog/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/events"
)

// Uploads is what ListingWatch reads of a deleted listing.
type Uploads interface {
	DeletedUploads(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error)
}

// ListingWatch hears listing events: Mount subscribes Deleted to
// listing.deleted and, when IndexNow is on, Changed to the listing events a
// search engine would see.
type ListingWatch struct {
	Prefix   string
	Listings Uploads
}

func (w ListingWatch) ServiceName() string { return w.Prefix + "CatalogListingWatch" }

// Changed sends the listing e names to the IndexNow queue.
func (w ListingWatch) Changed(ctx restate.Context, e events.Event) error {
	restate.ObjectSend(ctx, w.Prefix+service.IndexNowName, service.IndexNowQueue, "Add").Send(e.SubjectID)
	return nil
}

// Deleted has media release the photos the deleted listing e names no
// longer shows, whoever deleted it: the seller, or erasure.
func (w ListingWatch) Deleted(ctx restate.Context, e events.Event) error {
	ids, err := restate.Run(ctx, func(rc restate.RunContext) ([]uuid.UUID, error) {
		return w.Listings.DeletedUploads(rc, e.SubjectID)
	})
	if err != nil {
		return fmt.Errorf("read the uploads of listing %s: %w", e.SubjectID, err)
	}
	mediasvc.Release(ctx, w.Prefix, e.SubjectID, ids)
	return nil
}
