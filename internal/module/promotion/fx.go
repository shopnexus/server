package promotion

import (
	"go.uber.org/fx"

	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	promotiondb "shopnexus-remastered/internal/module/promotion/db/sqlc"
	promotionecho "shopnexus-remastered/internal/module/promotion/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

// Module provides the promotion module dependencies
var Module = fx.Module("promotion",
	fx.Provide(
		NewPromotionStorage,
		promotionbiz.NewPromotionBiz,
		promotionecho.NewHandler,
	),
	fx.Invoke(
		promotionecho.NewHandler,
	),
)

func NewPromotionStorage(pool pgsqlc.TxBeginner) promotionbiz.PromotionStorage {
	return pgsqlc.NewStorage(pool, promotiondb.New(pool))
}
