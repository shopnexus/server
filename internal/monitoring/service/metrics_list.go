package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

// listCalls is how many label lists one ListMetrics asks for at a time.
const listCalls = 8

// commonFrom is the fewest metrics a list names common labels for; a
// shorter list names each metric's labels in full.
const commonFrom = 5

// ListMetrics is UC-MOD-13 A1.
func (m *monitoring) ListMetrics(ctx context.Context, match string) (MetricList, error) {
	// A1. The names that hold match, at most MaxMetrics. A histogram is
	// stored as _bucket, _count and _sum, and listed once under its name.
	names, err := m.d.Metrics.Names(ctx)
	if err != nil {
		return MetricList{}, fmt.Errorf("list metric names: %w", err)
	}
	histogram := map[string]bool{}
	for _, n := range names {
		if base, ok := strings.CutSuffix(n, "_bucket"); ok {
			histogram[base] = true
		}
	}
	var picked []string
	for _, n := range names {
		name := n
		for _, part := range []string{"_bucket", "_count", "_sum"} {
			if base, ok := strings.CutSuffix(n, part); ok && histogram[base] {
				name = base
			}
		}
		if strings.Contains(name, match) && !slices.Contains(picked, name) {
			picked = append(picked, name)
		}
	}
	slices.Sort(picked)
	out := MetricList{Metrics: make([]domain.Metric, 0, min(len(picked), domain.MaxMetrics)), CommonLabels: []string{}}
	if len(picked) > domain.MaxMetrics {
		picked, out.Truncated = picked[:domain.MaxMetrics], true
	}
	meta, err := m.d.Metrics.Metadata(ctx)
	if err != nil {
		return MetricList{}, fmt.Errorf("read metric metadata: %w", err)
	}

	// Each metric's labels, a histogram's from its buckets.
	labels := make([][]string, len(picked))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(listCalls)
	for i, n := range picked {
		series := n
		if histogram[n] {
			series = n + "_bucket"
		}
		g.Go(func() error {
			l, err := m.d.Metrics.Labels(gctx, series)
			if err != nil {
				return fmt.Errorf("list labels of %s: %w", series, err)
			}
			labels[i] = l
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return MetricList{}, err
	}

	// In a long list, the labels most metrics have, such as the process's,
	// are named once; a metric without one of them says so.
	count := map[string]int{}
	for _, l := range labels {
		for _, name := range l {
			count[name]++
		}
	}
	for name, n := range count {
		if len(labels) >= commonFrom && 2*n > len(labels) {
			out.CommonLabels = append(out.CommonLabels, name)
		}
	}
	slices.Sort(out.CommonLabels)
	for i, n := range picked {
		own := slices.DeleteFunc(append([]string{}, labels[i]...), func(l string) bool { return slices.Contains(out.CommonLabels, l) })
		var lacks []string
		for _, c := range out.CommonLabels {
			if !slices.Contains(labels[i], c) {
				lacks = append(lacks, c)
			}
		}
		// A metric sent over OTLP has no metadata: a histogram has buckets
		// and a counter's name ends in _total.
		kind := meta[n].Type
		switch {
		case kind != "" && kind != "unknown":
		case histogram[n]:
			kind = "histogram"
		case strings.HasSuffix(n, "_total"):
			kind = "counter"
		default:
			kind = "gauge"
		}
		out.Metrics = append(out.Metrics, domain.Metric{Name: n, Type: kind, Help: meta[n].Help, Labels: own, Lacks: lacks})
	}
	return out, nil
}
