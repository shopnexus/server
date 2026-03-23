package analyticbiz

import (
	restate "github.com/restatedev/sdk-go"

	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

// AnalyticBizService wraps AnalyticBiz for Restate registration.
// All methods accept restate.Context and delegate to AnalyticBiz with context.Context.
type AnalyticBizService struct {
	biz *AnalyticBiz
}

func NewAnalyticBizService(biz *AnalyticBiz) *AnalyticBizService {
	return &AnalyticBizService{biz: biz}
}

func (s *AnalyticBizService) CreateInteraction(ctx restate.Context, params CreateInteractionParams) error {
	return s.biz.CreateInteraction(ctx, params)
}

func (s *AnalyticBizService) HandlePopularityEvent(ctx restate.Context, event analyticmodel.Interaction) error {
	return s.biz.HandlePopularityEvent(ctx, event)
}

func (s *AnalyticBizService) GetProductPopularity(ctx restate.Context, spuID uuid.UUID) (analyticdb.AnalyticProductPopularity, error) {
	return s.biz.GetProductPopularity(ctx, spuID)
}

func (s *AnalyticBizService) ListTopProductPopularity(ctx restate.Context, params sharedmodel.PaginationParams) ([]analyticdb.AnalyticProductPopularity, error) {
	return s.biz.ListTopProductPopularity(ctx, params)
}
