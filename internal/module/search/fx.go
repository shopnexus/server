package search

import (
	searchbiz "shopnexus-remastered/internal/module/search/biz"
	searchecho "shopnexus-remastered/internal/module/search/transport/echo"

	"go.uber.org/fx"
)

// Module provides the auth module dependencies
var Module = fx.Module("search",
	fx.Provide(
		searchbiz.NewSearchBiz,
		searchecho.NewHandler,
	),
)
