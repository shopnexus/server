// Package telemetry sets up logs, traces and metrics. Everything is exported
// over OTLP/HTTP to the endpoint in config; with no endpoint, spans still get
// ids but nothing leaves the process.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/shopnexus/server/internal/platform/config"
)

// Setup installs the global tracer, meter and logger providers and the W3C
// trace-context propagator. The returned function flushes and stops them.
func Setup(ctx context.Context, cfg config.Config, version string) (shutdown func(context.Context) error, err error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	// The instance id keeps each process its own series; without it two
	// replicas' counters merge into one that seems to reset at every point.
	host, _ := os.Hostname()
	res := resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(version),
		semconv.ServiceInstanceID(host),
		semconv.DeploymentEnvironmentName(string(cfg.Env)),
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
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}
	metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(cfg.OTLPEndpoint+"/v1/metrics"))
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}
	logExp, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(cfg.OTLPEndpoint+"/v1/logs"))
	if err != nil {
		return nil, fmt.Errorf("create log exporter: %w", err)
	}

	// Development samples every request; production sampling comes with deployment.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(10*time.Second),
			sdkmetric.WithProducer(runtime.NewProducer()))),
	)
	// Goroutines, heap, GOMAXPROCS and scheduler wait, plus CPU and memory
	// against the container's limits: the dashboard's saturation row.
	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nil, fmt.Errorf("start runtime metrics: %w", err)
	}
	registerProcessMetrics(mp)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
	)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetLoggerProvider(lp)
	clientTracers = newClientTracers(traceExp, string(cfg.Env))

	return func(ctx context.Context) error {
		errs := make([]error, 0, 3+len(clientTracers))
		errs = append(errs, tp.Shutdown(ctx), mp.Shutdown(ctx), lp.Shutdown(ctx))
		for _, ct := range clientTracers {
			errs = append(errs, ct.Shutdown(ctx))
		}
		return errors.Join(errs...)
	}, nil
}
