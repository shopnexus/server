package promotionbiz

import (
	"context"

	promotiondb "shopnexus-server/internal/module/promotion/db/sqlc"
	promotionmodel "shopnexus-server/internal/module/promotion/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"
)

// PromotionClient is the client interface for PromotionBiz, which is used by other modules to call PromotionBiz methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface PromotionClient -service PromotionBiz
type PromotionClient interface {
	// Promotion
	GetPromotion(ctx context.Context, params GetPromotionParams) (promotionmodel.Promotion, error)
	ListPromotion(ctx context.Context, params ListPromotionParams) (sharedmodel.PaginateResult[promotionmodel.Promotion], error)
	CreatePromotion(ctx context.Context, params CreatePromotionParams) (promotionmodel.Promotion, error)
	UpdatePromotion(ctx context.Context, params UpdatePromotionParams) (promotionmodel.Promotion, error)
	DeletePromotion(ctx context.Context, params DeletePromotionParams) error
}

type PromotionStorage = pgsqlc.Storage[*promotiondb.Queries]

// PromotionBiz implements the core business logic for the promotion module.
type PromotionBiz struct {
	storage PromotionStorage
}

// NewPromotionBiz creates a new PromotionBiz with the given dependencies.
func NewPromotionBiz(storage PromotionStorage) *PromotionBiz {
	return &PromotionBiz{storage: storage}
}
