package promotionbiz

import (
	restate "github.com/restatedev/sdk-go"

	promotionmodel "shopnexus-server/internal/module/promotion/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// PromotionBizService wraps PromotionBiz for Restate registration.
// All methods accept restate.Context and delegate to PromotionBiz with context.Context.
type PromotionBizService struct {
	biz *PromotionBiz
}

func NewPromotionBizService(biz *PromotionBiz) *PromotionBizService {
	return &PromotionBizService{biz: biz}
}

func (s *PromotionBizService) GetPromotion(ctx restate.Context, params GetPromotionParams) (promotionmodel.Promotion, error) {
	return s.biz.GetPromotion(ctx, params)
}

func (s *PromotionBizService) ListPromotion(ctx restate.Context, params ListPromotionParams) (sharedmodel.PaginateResult[promotionmodel.Promotion], error) {
	return s.biz.ListPromotion(ctx, params)
}

func (s *PromotionBizService) CreatePromotion(ctx restate.Context, params CreatePromotionParams) (promotionmodel.Promotion, error) {
	return s.biz.CreatePromotion(ctx, params)
}

func (s *PromotionBizService) UpdatePromotion(ctx restate.Context, params UpdatePromotionParams) (promotionmodel.Promotion, error) {
	return s.biz.UpdatePromotion(ctx, params)
}

func (s *PromotionBizService) DeletePromotion(ctx restate.Context, params DeletePromotionParams) error {
	return s.biz.DeletePromotion(ctx, params)
}
