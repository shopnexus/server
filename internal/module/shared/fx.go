package shared

import (
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"

	"go.uber.org/fx"
)

// Module provides the shared module dependencies
var Module = fx.Module("shared",
	fx.Provide(
		sharedbiz.NewSharedBiz,
	),
)
