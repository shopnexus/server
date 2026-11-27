package service

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

// defaultLogRange is how far back a log query reaches when it names no
// range.
const defaultLogRange = time.Hour

// QueryLogs is UC-MOD-13 step 3, masked for NFR-PRIV-02.
func (m *monitoring) QueryLogs(ctx context.Context, q Query, limit int) (LogsResult, error) {
	// 3. The range within the limits; E1 past them.
	rng := q.Range
	if rng == 0 {
		rng = defaultLogRange
	}
	span, err := domain.NewSpan(m.end(q), rng, 0)
	if err != nil {
		return LogsResult{}, err
	}
	limit = min(max(limit, 1), domain.MaxLines)
	got, err := m.d.Logs.Query(ctx, q.Text, span, limit)
	if err != nil {
		return LogsResult{}, fmt.Errorf("query logs: %w", err)
	}
	out := LogsResult{Query: q.Text, Lines: []domain.LogLine{}, Series: []domain.Series{}}
	// What reaches the one who asked is masked (NFR-PRIV-02).
	lines := got.Lines
	if len(lines) > limit {
		lines, out.Truncated = lines[:limit], true
	}
	for _, l := range lines {
		fields := maps.Clone(l.Fields)
		for k, v := range fields {
			fields[k] = domain.Mask(v)
		}
		out.Lines = append(out.Lines, domain.LogLine{At: l.At, Fields: fields, Line: domain.Mask(l.Line)})
	}
	series, truncated := cut(got.Series)
	out.Truncated = out.Truncated || truncated
	for _, s := range series {
		labels := maps.Clone(s.Labels)
		for k, v := range labels {
			labels[k] = domain.Mask(v)
		}
		out.Series = append(out.Series, domain.Series{Labels: labels, Points: s.Points})
	}
	return out, nil
}
