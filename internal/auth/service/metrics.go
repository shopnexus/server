package service

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/platform/telemetry"
)

var meter = otel.Meter("github.com/shopnexus/server2/internal/auth")

// The spec's auth counters; handler counts login_total. Instruments from the global meter bind to the
// provider telemetry.Setup installs, even when they were created first.
var (
	otpSent      = telemetry.Must(meter.Int64Counter("otp_sent_total", metric.WithDescription("OTP codes sent")))
	refreshReuse = telemetry.Must(meter.Int64Counter("refresh_reuse_total", metric.WithDescription("refresh tokens presented after they were rotated")))
)
