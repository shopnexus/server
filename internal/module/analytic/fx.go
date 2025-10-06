package analytic

import (
	analyticbiz "shopnexus-remastered/internal/module/analytic/biz"
	analyticecho "shopnexus-remastered/internal/module/analytic/transport/echo"

	"go.uber.org/fx"
)

// Module provides the analytic module dependencies
var Module = fx.Module("analytic",
	fx.Provide(
		analyticbiz.NewAnalyticBiz,
		analyticecho.NewHandler,
	),
	fx.Invoke(
		analyticecho.NewHandler,
	),
)
