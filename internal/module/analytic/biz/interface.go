package analyticbiz

import (
	"context"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/pubsub"
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
	CreateInteraction(ctx context.Context, params CreateInteractionParams) error
	HandlePopularityEvent(ctx context.Context, event analyticmodel.Interaction) error
	GetProductPopularity(ctx context.Context, spuID uuid.UUID) (analyticdb.AnalyticProductPopularity, error)
	ListTopProductPopularity(ctx context.Context, params sharedmodel.PaginationParams) ([]analyticdb.AnalyticProductPopularity, error)
}

type AnalyticStorage = pgsqlc.Storage[*analyticdb.Queries]

type AnalyticBiz struct {
	storage           AnalyticStorage
	pubsub            pubsub.Client
	promotion         *promotionbiz.PromotionBiz
	popularityWeights map[string]float64
}

// NewAnalyticBiz creates a new instance of AnalyticBiz.
func NewAnalyticBiz(
	config *config.Config,
	storage AnalyticStorage,
	pubsub pubsub.Client,
	promotionBiz *promotionbiz.PromotionBiz,
) *AnalyticBiz {
	return &AnalyticBiz{
		storage:           storage,
		pubsub:            pubsub,
		promotion:         promotionBiz,
		popularityWeights: analyticconfig.DefaultPopularityWeights().WeightMap(),
	}
}
