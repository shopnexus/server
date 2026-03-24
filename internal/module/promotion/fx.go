package promotion

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	promotiondb "shopnexus-server/internal/module/promotion/db/sqlc"
	promotionecho "shopnexus-server/internal/module/promotion/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the promotion module dependencies
var Module = fx.Module("promotion",
	fx.Provide(
		NewPromotionStorage,
		promotionbiz.NewPromotionBiz,
		NewPromotionClient,
		promotionecho.NewHandler,
	),
	fx.Invoke(
		promotionecho.NewHandler,
	),
)

// NewPromotionStorage creates a new promotion storage backed by PostgreSQL.
func NewPromotionStorage(pool pgsqlc.TxBeginner) promotionbiz.PromotionStorage {
	return pgsqlc.NewStorage(pool, promotiondb.New(pool))
}

// NewPromotionClient creates a Restate-backed client for the promotion module.
func NewPromotionClient(cfg *config.Config) promotionbiz.PromotionClient {
	return promotionbiz.NewPromotionBizRestateClient(cfg.Restate.IngressAddress)
}
