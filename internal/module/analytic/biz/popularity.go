package analyticbiz

import (
	"fmt"
	"log/slog"

	restate "github.com/restatedev/sdk-go"

	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

// HandlePopularityEvent updates product popularity scores based on an interaction event.
func (b *AnalyticBiz) HandlePopularityEvent(ctx restate.Context, event analyticmodel.Interaction) error {
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
		ID:            spuID,
		Score:         weight,
		ViewCount:     viewCount,
		PurchaseCount: purchaseCount,
		FavoriteCount: favoriteCount,
		CartCount:     cartCount,
		ReviewCount:   reviewCount,
	}); err != nil {
		return fmt.Errorf("upsert product popularity: %w", err)
	}

	return nil
}

// GetProductPopularity returns the popularity metrics for the given product SPU.
func (b *AnalyticBiz) GetProductPopularity(ctx restate.Context, spuID uuid.UUID) (analyticdb.AnalyticProductPopularity, error) {
	return b.storage.Querier().GetProductPopularityByID(ctx, spuID)
}

// ListTopProductPopularity returns the top products ranked by popularity score.
func (b *AnalyticBiz) ListTopProductPopularity(ctx restate.Context, params sharedmodel.PaginationParams) ([]analyticdb.AnalyticProductPopularity, error) {
	params = params.Constrain()
	return b.storage.Querier().ListTopProductPopularity(ctx, analyticdb.ListTopProductPopularityParams{
		Limit:  params.Limit,
		Offset: params.Offset(),
	})
}
