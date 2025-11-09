package catalogbiz

import (
	"shopnexus-remastered/internal/infras/cachestruct"
	"shopnexus-remastered/internal/infras/pubsub"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	searchbiz "shopnexus-remastered/internal/module/search/biz"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
)

type CatalogBiz struct {
	cache     cachestruct.Client
	pubsub    pubsub.Client
	storage   pgsqlc.Storage
	common    *commonbiz.Commonbiz
	promotion *promotionbiz.PromotionBiz
	search    *searchbiz.SearchBiz
}

func NewCatalogBiz(
	cache cachestruct.Client,
	pubsub pubsub.Client,
	storage pgsqlc.Storage,
	common *commonbiz.Commonbiz,
	promotion *promotionbiz.PromotionBiz,
	search *searchbiz.SearchBiz,
) *CatalogBiz {
	return &CatalogBiz{
		cache:     cache,
		pubsub:    pubsub.Group("catalog"),
		storage:   storage,
		common:    common,
		promotion: promotion,
		search:    search,
	}
}
