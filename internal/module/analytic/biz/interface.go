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

// AnalyticBiz is the client interface for AnalyticBizImpl, which is used by other modules to call AnalyticBizImpl methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface AnalyticBiz -service AnalyticBiz
type AnalyticBiz interface {
	// Interaction
	CreateInteraction(ctx context.Context, params CreateInteractionParams) error

	// Popularity
	HandlePopularityEvent(ctx context.Context, event analyticmodel.Interaction) error
	GetProductPopularity(ctx context.Context, spuID uuid.UUID) (analyticdb.AnalyticProductPopularity, error)
	ListTopProductPopularity(ctx context.Context, params sharedmodel.PaginationParams) ([]analyticdb.AnalyticProductPopularity, error)
}

type AnalyticStorage = pgsqlc.Storage[*analyticdb.Queries]

// AnalyticBizImpl implements the core business logic for the analytic module.
type AnalyticBizImpl struct {
	storage           AnalyticStorage
	promotion         promotionbiz.PromotionBiz
	popularityWeights map[string]float64
}

// NewAnalyticBiz creates a new AnalyticBizImpl with the given dependencies.
func NewAnalyticBiz(
	config *config.Config,
	storage AnalyticStorage,
	promotionBiz promotionbiz.PromotionBiz,
) *AnalyticBizImpl {
	return &AnalyticBizImpl{
		storage:           storage,
		promotion:         promotionBiz,
		popularityWeights: analyticconfig.DefaultPopularityWeights().WeightMap(),
	}
}
