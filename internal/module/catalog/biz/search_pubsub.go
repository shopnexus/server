package catalogbiz

import (
	"context"
	"errors"

	"shopnexus-remastered/internal/infras/pubsub"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
)

func (b *CatalogBiz) InitPubsub() error {
	return errors.Join(
		b.pubsub.Subscribe(analyticmodel.TopicAnalyticInteraction, pubsub.DecodeWrap(b.AddInteraction)),
	)
}

type AddInteractionParams = analyticmodel.Interaction

func (b *CatalogBiz) AddInteraction(ctx context.Context, params AddInteractionParams) error {
	b.searchClient.mu.Lock()
	defer b.searchClient.mu.Unlock()

	// buffer the event
	b.searchClient.buffer = append(b.searchClient.buffer, params)

	// if reached batch size, process events
	if len(b.searchClient.buffer) >= b.searchClient.batchSize {
		toInsert := b.searchClient.buffer
		b.searchClient.buffer = make([]analyticmodel.Interaction, 0, b.searchClient.batchSize) // reset buffer

		// Refresh customer feeds
		if err := b.ProcessEvents(ctx, toInsert); err != nil {
			return err
		}
	}
	return nil
}
