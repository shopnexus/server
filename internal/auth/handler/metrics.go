package handler

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/platform/telemetry"
)

// logins is the spec's login_total, on the meter service counts its own on.
var logins = telemetry.Must(otel.Meter("github.com/shopnexus/server2/internal/auth").
	Int64Counter("login_total", metric.WithDescription("sign-in attempts by method and result")))
