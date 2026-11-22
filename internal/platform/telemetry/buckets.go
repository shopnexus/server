package telemetry

import "go.opentelemetry.io/otel/metric"

// Seconds gives a duration histogram the semantic conventions' buckets, 5 ms
// to 10 s. The SDK's default buckets are for milliseconds.
var Seconds = metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10)
