package catalogbiz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"shopnexus-server/internal/infras/pubsub"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
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

		// Remove old recommendations for all affected accounts
		seen := make(map[uuid.UUID]struct{})
		for _, ev := range toInsert {
			if !ev.AccountID.Valid {
				continue
			}
			if _, ok := seen[ev.AccountID.UUID]; ok {
				continue
			}
			seen[ev.AccountID.UUID] = struct{}{}
			if err := b.cache.Delete(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendProduct, ev.AccountID.UUID.String())); err != nil {
				slog.Error("failed to reset feed offset for account", slog.String("account_id", ev.AccountID.UUID.String()), slog.Any("error", err))
			}
		}
	}
	return nil
}
