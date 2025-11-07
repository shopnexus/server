package common

import (
	"go.uber.org/fx"

	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commonecho "shopnexus-remastered/internal/module/common/transport/echo"
)

// Module provides the common module dependencies
var Module = fx.Module("common",
	fx.Provide(
		commonbiz.Newcommonbiz,
		commonecho.NewHandler,
	),
	fx.Invoke(
		commonecho.NewHandler,
	),
)
