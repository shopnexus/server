package service

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

// listCalls is how many label lists one ListMetrics asks for at a time.
const listCalls = 8

// ListMetrics is UC-MOD-13 A1.
func (m *monitoring) ListMetrics(ctx context.Context, match string) (MetricList, error) {
	// A1. The names that hold match, at most MaxMetrics.
	names, err := m.d.Metrics.Names(ctx)
	if err != nil {
		return MetricList{}, fmt.Errorf("list metric names: %w", err)
	}
	var picked []string
	for _, n := range names {
		if strings.Contains(n, match) {
			picked = append(picked, n)
		}
	}
	out := MetricList{Metrics: make([]domain.Metric, 0, min(len(picked), domain.MaxMetrics))}
	if len(picked) > domain.MaxMetrics {
		picked, out.Truncated = picked[:domain.MaxMetrics], true
	}
	meta, err := m.d.Metrics.Metadata(ctx)
	if err != nil {
		return MetricList{}, fmt.Errorf("read metric metadata: %w", err)
	}
	// Each metric's labels. A histogram's metadata is under its name
	// without _bucket, _count or _sum.
	labels := make([][]string, len(picked))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(listCalls)
	for i, n := range picked {
		g.Go(func() error {
			l, err := m.d.Metrics.Labels(gctx, n)
			if err != nil {
				return fmt.Errorf("list labels of %s: %w", n, err)
			}
			labels[i] = l
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return MetricList{}, err
	}
	for i, n := range picked {
		md, ok := meta[n]
		if !ok {
			for _, suffix := range []string{"_bucket", "_count", "_sum", "_total"} {
				if base, cut := strings.CutSuffix(n, suffix); cut {
					if md, ok = meta[base]; ok {
						break
					}
				}
			}
		}
		if md.Type == "" {
			md.Type = "unknown"
		}
		l := labels[i]
		if l == nil {
			l = []string{}
		}
		out.Metrics = append(out.Metrics, domain.Metric{Name: n, Type: md.Type, Help: md.Help, Labels: l})
	}
	return out, nil
}
