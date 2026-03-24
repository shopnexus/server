package promotionbiz

import (
	"context"

	catalogmodel "shopnexus-server/internal/module/catalog/model"
	promotiondb "shopnexus-server/internal/module/promotion/db/sqlc"
	promotionmodel "shopnexus-server/internal/module/promotion/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

// PromotionBiz is the client interface for PromotionBizImpl, which is used by other modules to call PromotionBizImpl methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface PromotionBiz -service PromotionBiz
type PromotionBiz interface {
	// Promotion
	GetPromotion(ctx context.Context, params GetPromotionParams) (promotionmodel.Promotion, error)
	ListPromotion(ctx context.Context, params ListPromotionParams) (sharedmodel.PaginateResult[promotionmodel.Promotion], error)
	CreatePromotion(ctx context.Context, params CreatePromotionParams) (promotionmodel.Promotion, error)
	UpdatePromotion(ctx context.Context, params UpdatePromotionParams) (promotionmodel.Promotion, error)
	DeletePromotion(ctx context.Context, params DeletePromotionParams) error
	CalculatePromotedPrices(ctx context.Context, params CalculatePromotedPricesParams) (map[uuid.UUID]*catalogmodel.OrderPrice, error)
}

type PromotionStorage = pgsqlc.Storage[*promotiondb.Queries]

// PromotionBizImpl implements the core business logic for the promotion module.
type PromotionBizImpl struct {
	storage PromotionStorage
}

// NewPromotionBiz creates a new PromotionBizImpl with the given dependencies.
func NewPromotionBiz(storage PromotionStorage) *PromotionBizImpl {
	return &PromotionBizImpl{storage: storage}
}
