package auth

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("github.com/shopnexus/server2/internal/auth")

// The spec's auth counters. Instruments from the global meter bind to the
// provider telemetry.Setup installs, even when they were created first.
var (
	otpSent      = counter("otp_sent_total", "OTP codes sent")
	logins       = counter("login_total", "sign-in attempts by method and result")
	refreshReuse = counter("refresh_reuse_total", "refresh tokens presented after they were rotated")
)

func counter(name, description string) metric.Int64Counter {
	c, err := meter.Int64Counter(name, metric.WithDescription(description))
	if err != nil {
		panic(err)
	}
	return c
}
