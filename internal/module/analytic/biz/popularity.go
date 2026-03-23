package analyticbiz

import (
	"fmt"
	"log/slog"

	restate "github.com/restatedev/sdk-go"

	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

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
		SpuID:         spuID,
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

func (b *AnalyticBiz) GetProductPopularity(ctx restate.Context, spuID uuid.UUID) (analyticdb.AnalyticProductPopularity, error) {
	return b.storage.Querier().GetProductPopularity(ctx, spuID)
}

func (b *AnalyticBiz) ListTopProductPopularity(ctx restate.Context, params sharedmodel.PaginationParams) ([]analyticdb.AnalyticProductPopularity, error) {
	params = params.Constrain()
	return b.storage.Querier().ListTopProductPopularity(ctx, analyticdb.ListTopProductPopularityParams{
		Limit:  null.Int32From(params.Limit.Int32),
		Offset: params.Offset(),
	})
}
