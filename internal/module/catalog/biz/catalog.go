package catalogbiz

import (
	"shopnexus-remastered/internal/client/cachestruct"
	"shopnexus-remastered/internal/client/pubsub"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	searchbiz "shopnexus-remastered/internal/module/search/biz"
	"shopnexus-remastered/internal/utils/pgutil"
)

type CatalogBiz struct {
	cache     cachestruct.Client
	pubsub    pubsub.Client
	storage   *pgutil.Storage
	promotion *promotionbiz.PromotionBiz
	search    *searchbiz.SearchBiz
}

func NewCatalogBiz(
	cache cachestruct.Client,
	pubsub pubsub.Client,
	storage *pgutil.Storage,
	promotion *promotionbiz.PromotionBiz,
	search *searchbiz.SearchBiz,
) *CatalogBiz {
	return &CatalogBiz{
		cache:     cache,
		pubsub:    pubsub.Group("catalog"),
		storage:   storage,
		promotion: promotion,
		search:    search,
	}
}
