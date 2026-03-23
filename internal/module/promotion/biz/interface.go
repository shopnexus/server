package promotionbiz

import (
	"context"

	promotionmodel "shopnexus-server/internal/module/promotion/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// PromotionClient is the client interface for PromotionBiz, which is used by other modules to call PromotionBiz methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface PromotionClient -service PromotionBiz
type PromotionClient interface {
	GetPromotion(ctx context.Context, params GetPromotionParams) (promotionmodel.Promotion, error)
	ListPromotion(ctx context.Context, params ListPromotionParams) (sharedmodel.PaginateResult[promotionmodel.Promotion], error)
	CreatePromotion(ctx context.Context, params CreatePromotionParams) (promotionmodel.Promotion, error)
	UpdatePromotion(ctx context.Context, params UpdatePromotionParams) (promotionmodel.Promotion, error)
	DeletePromotion(ctx context.Context, params DeletePromotionParams) error
}
