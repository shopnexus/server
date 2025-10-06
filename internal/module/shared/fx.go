package shared

import (
	"go.uber.org/fx"

	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
	sharedecho "shopnexus-remastered/internal/module/shared/transport/echo"
)

// Module provides the shared module dependencies
var Module = fx.Module("shared",
	fx.Provide(
		sharedbiz.NewSharedBiz,
		sharedecho.NewHandler,
	),
	fx.Invoke(
		sharedecho.NewHandler,
	),
)
