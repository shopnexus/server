package analyticbiz

import (
	"context"

	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	sharedmodel "shopnexus-server/internal/shared/model"

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
