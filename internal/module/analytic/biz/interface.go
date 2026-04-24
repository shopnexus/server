package analyticbiz

import (
	"context"

	"shopnexus-server/config"
	analyticconfig "shopnexus-server/internal/module/analytic/config"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

// AnalyticBiz is the client interface for AnalyticBizHandler, which is used by other modules to call AnalyticBizHandler methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface AnalyticBiz -service Analytic
type AnalyticBiz interface {
	// Interaction
	CreateInteraction(ctx context.Context, params CreateInteractionParams) error

	// Popularity
	HandlePopularityEvent(ctx context.Context, event analyticmodel.Interaction) error
	GetProductPopularity(ctx context.Context, spuID uuid.UUID) (analyticdb.AnalyticProductPopularity, error)
	ListTopProductPopularity(
		ctx context.Context,
		params sharedmodel.PaginationParams,
	) ([]analyticdb.AnalyticProductPopularity, error)

	// Dashboard
	GetSellerDashboard(
		ctx context.Context,
		params analyticmodel.GetSellerDashboardParams,
	) (analyticmodel.SellerDashboard, error)
}

type AnalyticStorage = pgsqlc.Storage[*analyticdb.Queries]

// AnalyticHandler implements the core business logic for the analytic module.
type AnalyticHandler struct {
	storage           AnalyticStorage
	promotion         promotionbiz.PromotionBiz
	popularityWeights map[analyticmodel.Event]float64
}

func (b *AnalyticHandler) ServiceName() string {
	return "Analytic"
}

// NewAnalyticHandler creates a new AnalyticHandler with the given dependencies.
func NewAnalyticHandler(
	config *config.Config,
	storage AnalyticStorage,
	promotionBiz promotionbiz.PromotionBiz,
) *AnalyticHandler {
	return &AnalyticHandler{
		storage:           storage,
		promotion:         promotionBiz,
		popularityWeights: analyticconfig.DefaultPopularityWeights().WeightMap(),
	}
}
