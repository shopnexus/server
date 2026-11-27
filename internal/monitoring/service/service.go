// Package service holds module monitoring's use cases: an admin, through
// the console agent, reads the system's health and runs PromQL and LogQL
// queries within the limits of ADR-0007. Every one is a direct use case
// and changes nothing.
package service

import (
	"context"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
	"github.com/shopnexus/server/internal/monitoring/repository"
)

// Service is module monitoring's contract.
type Service interface {
	// Health reads the system's health over window, each figure beside
	// the same window a week before.
	//
	// UC-MOD-13
	Health(ctx context.Context, window time.Duration) (Health, error)
	// ListMetrics lists the metrics whose name holds match, or all, with
	// their type, help and labels.
	//
	// UC-MOD-13
	ListMetrics(ctx context.Context, match string) (MetricList, error)
	// QueryMetrics runs a PromQL query at end, or over rng up to end a
	// point each step.
	//
	// UC-MOD-13
	QueryMetrics(ctx context.Context, q Query, step time.Duration) (MetricsResult, error)
	// QueryLogs runs a LogQL query over rng up to end: at most limit
	// lines, masked, or a metric query's series.
	//
	// UC-MOD-13 · NFR-PRIV-02
	QueryLogs(ctx context.Context, q Query, limit int) (LogsResult, error)
}

// Query is a query and when it looks: End zero is now, Range zero is one
// time.
type Query struct {
	Text  string
	Range time.Duration
	End   time.Time
}

// Deps is what the use cases need.
type Deps struct {
	Metrics repository.Metrics
	Logs    repository.Logs
	Now     func() time.Time
}

type monitoring struct{ d Deps }

func New(d Deps) Service { return &monitoring{d: d} }

// MetricsResult is a PromQL query's answer.
type MetricsResult struct {
	Query     string          `json:"query"`
	Step      *string         `json:"step"`
	Series    []domain.Series `json:"series"`
	Truncated bool            `json:"truncated"`
}

// LogsResult is a LogQL query's answer: lines or series.
type LogsResult struct {
	Query     string           `json:"query"`
	Lines     []domain.LogLine `json:"lines"`
	Series    []domain.Series  `json:"series"`
	Truncated bool             `json:"truncated"`
}

// MetricList is the metrics a list found.
type MetricList struct {
	Metrics []domain.Metric `json:"metrics"`
	// CommonLabels are the labels most listed metrics have, in a list of
	// five or more.
	CommonLabels []string `json:"common_labels"`
	Truncated    bool     `json:"truncated"`
}

// end is q's end, now when it has none.
func (m *monitoring) end(q Query) time.Time {
	if q.End.IsZero() {
		return m.d.Now().UTC()
	}
	return q.End.UTC()
}

// cut keeps the first MaxSeries series.
func cut(s []domain.Series) ([]domain.Series, bool) {
	if len(s) > domain.MaxSeries {
		return s[:domain.MaxSeries], true
	}
	return s, false
}
