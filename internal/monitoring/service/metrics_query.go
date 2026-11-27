package service

import (
	"context"
	"fmt"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

// QueryMetrics is UC-MOD-13 step 3.
func (m *monitoring) QueryMetrics(ctx context.Context, q Query, step time.Duration) (MetricsResult, error) {
	// 3. The range and the step within the limits; E1 past them.
	span, err := domain.NewSpan(m.end(q), q.Range, step)
	if err != nil {
		return MetricsResult{}, err
	}
	series, err := m.d.Metrics.Query(ctx, q.Text, span)
	if err != nil {
		return MetricsResult{}, fmt.Errorf("query metrics: %w", err)
	}
	out := MetricsResult{Query: q.Text}
	out.Series, out.Truncated = cut(series)
	if span.Step > 0 {
		s := domain.FormatDuration(span.Step)
		out.Step = &s
	}
	return out, nil
}
