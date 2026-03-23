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

// AnalyticClient is the client interface for AnalyticBiz, which is used by other modules to call AnalyticBiz methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface AnalyticClient -service AnalyticBiz
type AnalyticClient interface {
	// Interaction
	CreateInteraction(ctx context.Context, params CreateInteractionParams) error

	// Popularity
	HandlePopularityEvent(ctx context.Context, event analyticmodel.Interaction) error
	GetProductPopularity(ctx context.Context, spuID uuid.UUID) (analyticdb.AnalyticProductPopularity, error)
	ListTopProductPopularity(ctx context.Context, params sharedmodel.PaginationParams) ([]analyticdb.AnalyticProductPopularity, error)
}

type AnalyticStorage = pgsqlc.Storage[*analyticdb.Queries]

// AnalyticBiz implements the core business logic for the analytic module.
type AnalyticBiz struct {
	storage           AnalyticStorage
	promotion         *promotionbiz.PromotionBiz
	popularityWeights map[string]float64
}

// NewAnalyticBiz creates a new AnalyticBiz with the given dependencies.
func NewAnalyticBiz(
	config *config.Config,
	storage AnalyticStorage,
	promotionBiz *promotionbiz.PromotionBiz,
) *AnalyticBiz {
	return &AnalyticBiz{
		storage:           storage,
		promotion:         promotionBiz,
		popularityWeights: analyticconfig.DefaultPopularityWeights().WeightMap(),
	}
}
