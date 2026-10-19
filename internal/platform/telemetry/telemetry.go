// Package telemetry sets up logs, traces and metrics. Everything is exported
// over OTLP/HTTP to the endpoint in config; with no endpoint, spans still get
// ids but nothing leaves the process.
package telemetry

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/shopnexus/server2/internal/platform/config"
)

// Setup installs the global tracer, meter and logger providers and the W3C
// trace-context propagator. The returned function flushes and stops them.
func Setup(ctx context.Context, cfg config.Config, version string) (shutdown func(context.Context) error, err error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	res := resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(version),
		semconv.DeploymentEnvironmentName(cfg.Env),
	)
	if cfg.OTLPEndpoint == "" {
		// No exporter, but a real provider: spans still get ids, so logs,
		// error bodies and the Trace-Id header stay correlated.
		tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		return tp.Shutdown, nil
	}

	traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint+"/v1/traces"))
	if err != nil {
		return nil, err
	}
	metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(cfg.OTLPEndpoint+"/v1/metrics"))
	if err != nil {
		return nil, err
	}
	logExp, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(cfg.OTLPEndpoint+"/v1/logs"))
	if err != nil {
		return nil, err
	}

	// Development samples every request; production sampling comes with deployment.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(10*time.Second))),
	)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
	)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	global.SetLoggerProvider(lp)

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx), lp.Shutdown(ctx))
	}, nil
}
