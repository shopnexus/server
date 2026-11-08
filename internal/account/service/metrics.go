package service

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/platform/telemetry"
)

// refreshReuse is the spec's refresh_reuse_total; handler counts
// login_total on the same meter. An instrument from the global meter binds to
// the provider telemetry.Setup installs, even when it was created first.
var refreshReuse = telemetry.Must(otel.Meter("github.com/shopnexus/server2/internal/account").
	Int64Counter("refresh_reuse_total", metric.WithDescription("refresh tokens presented after they were rotated")))
