package catalogbiz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"shopnexus-remastered/internal/infras/pubsub"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
)

func (b *CatalogBiz) InitPubsub() error {
	return errors.Join(
		b.pubsub.Subscribe(analyticmodel.TopicAnalyticInteraction, pubsub.DecodeWrap(b.AddInteraction)),
	)
}

type AddInteractionParams = analyticmodel.Interaction

func (b *CatalogBiz) AddInteraction(ctx context.Context, params AddInteractionParams) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// buffer the event
	// WIP: storing buffer in memory kinda sucks, but good enough for now
	b.buffer = append(b.buffer, params)

	// if reached batch size, process events
	if len(b.buffer) >= b.batchSize {
		toInsert := b.buffer
		b.buffer = make([]analyticmodel.Interaction, 0, b.batchSize) // reset buffer

		// Refresh customer feeds
		if err := b.ProcessEvents(ctx, toInsert); err != nil {
			return err
		}

		// Remove all old recommendations
		if params.AccountID.Valid {
			if err := b.cache.Delete(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendProduct, params.AccountID.UUID.String())); err != nil {
				slog.Error("failed to reset feed offset for account", slog.String("account_id", params.AccountID.UUID.String()), slog.Any("error", err))
			}
		}
	}
	return nil
}
