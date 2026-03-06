package analyticbiz

import (
	"context"
	"fmt"
	"log/slog"

	analyticdb "shopnexus-remastered/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"

	"shopnexus-remastered/internal/infras/pubsub"
)

func (b *AnalyticBiz) HandlePopularityEvent(ctx context.Context, event analyticmodel.Interaction) error {
	if event.RefType != analyticdb.AnalyticInteractionRefTypeProduct {
		return nil
	}

	spuID, err := uuid.Parse(event.RefID)
	if err != nil {
		slog.Warn("invalid spu_id in interaction event", "ref_id", event.RefID, "error", err)
		return nil
	}

	weight, ok := b.popularityWeights[event.EventType]
	if !ok {
		return nil
	}

	var viewCount, purchaseCount, favoriteCount, cartCount, reviewCount int64
	switch event.EventType {
	case "view", "view_bounce":
		viewCount = 1
	case "purchase":
		purchaseCount = 1
	case "add_to_favorites":
		favoriteCount = 1
	case "add_to_cart", "remove_from_cart":
		cartCount = 1
	case "write_review", "rating_high", "rating_medium", "rating_low":
		reviewCount = 1
	}

	if _, err := b.storage.Querier().UpsertProductPopularity(ctx, analyticdb.UpsertProductPopularityParams{
		SpuID:         spuID,
		Score:         weight,
		ViewCount:     viewCount,
		PurchaseCount: purchaseCount,
		FavoriteCount: favoriteCount,
		CartCount:     cartCount,
		ReviewCount:   reviewCount,
	}); err != nil {
		return fmt.Errorf("failed to upsert product popularity: %w", err)
	}

	return nil
}

func (b *AnalyticBiz) GetProductPopularity(ctx context.Context, spuID uuid.UUID) (analyticdb.AnalyticProductPopularity, error) {
	return b.storage.Querier().GetProductPopularity(ctx, spuID)
}

func (b *AnalyticBiz) ListTopProductPopularity(ctx context.Context, params sharedmodel.PaginationParams) ([]analyticdb.AnalyticProductPopularity, error) {
	params = params.Constrain()
	return b.storage.Querier().ListTopProductPopularity(ctx, analyticdb.ListTopProductPopularityParams{
		Limit:  null.Int32From(params.Limit.Int32),
		Offset: params.Offset(),
	})
}

func (b *AnalyticBiz) InitPopularityPubsub() error {
	return b.pubsub.Subscribe(analyticmodel.TopicAnalyticInteraction, pubsub.DecodeWrap(func(ctx context.Context, event analyticmodel.Interaction) error {
		if err := b.HandlePopularityEvent(ctx, event); err != nil {
			slog.Error("failed to handle popularity event", "error", err)
		}
		return nil
	}))
}
