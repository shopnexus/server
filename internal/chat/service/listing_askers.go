package service

import (
	"context"
	"fmt"
	"time"
	"uuid"
)

// ListingAsker is someone who wrote to a seller about a listing, since
// their first message that names it.
type ListingAsker struct {
	ID    uuid.UUID
	Since time.Time
}

// ListingAskers reads the people who wrote to the seller about the seller's
// listings among ids, in one query: each listing's askers, earliest first.
// A listing nobody wrote about has no entry.
func (c *Chat) ListingAskers(ctx context.Context, seller uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]ListingAsker, error) {
	rows, err := c.chats.ListingSenders(ctx, seller, ids)
	if err != nil {
		return nil, fmt.Errorf("read listing senders: %w", err)
	}
	out := map[uuid.UUID][]ListingAsker{}
	for _, r := range rows {
		out[r.ListingID] = append(out[r.ListingID], ListingAsker{ID: r.SenderID, Since: r.First})
	}
	return out, nil
}
