// Package repository reads module monitoring's two sources over HTTP: the
// metrics from Prometheus's query API and the logs from Loki's. A source
// that refuses a query answers domain.ErrQueryInvalid with its own words;
// one that is down or not set up answers domain.ErrUnavailable.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

// Metrics is Prometheus's query API.
type Metrics interface {
	// Query runs query over span: at span.End, or as a range.
	Query(ctx context.Context, query string, span domain.Span) ([]domain.Series, error)
	// Names lists every metric's name.
	Names(ctx context.Context) ([]string, error)
	// Metadata is each metric's type and help, by name.
	Metadata(ctx context.Context) (map[string]Meta, error)
	// Labels lists the labels of metric's series over the last hour.
	Labels(ctx context.Context, metric string) ([]string, error)
}

// Meta is a metric's type and help.
type Meta struct {
	Type, Help string
}

// Logs is Loki's query API.
type Logs interface {
	// Query runs a LogQL query over span: a log query's newest lines, at
	// most limit, or a metric query's series.
	Query(ctx context.Context, query string, span domain.Span, limit int) (Logged, error)
}

// Logged is what a log query found: lines, or a metric query's series.
type Logged struct {
	Lines  []domain.LogLine
	Series []domain.Series
}

// maxBody is the most a source's answer may weigh; a query that asks for
// more should aggregate.
const maxBody = 16 << 20

// source calls one of the two APIs.
type source struct {
	base string
	http *http.Client
}

// get sends a GET of path with params and decodes the JSON answer into
// dst. bad reads a refused query's message from the answer's body.
func (s source) get(ctx context.Context, path string, params url.Values, dst any, bad func([]byte) string) error {
	if s.base == "" {
		return domain.ErrUnavailable.WithCause(errors.New("no address is set up"))
	}
	ctx, cancel := context.WithTimeout(ctx, domain.QueryTimeout+5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+path+"?"+params.Encode(), http.NoBody)
	if err != nil {
		return fmt.Errorf("build the request: %w", err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.Refused(fmt.Sprintf("the query ran past %s; narrow it", domain.QueryTimeout))
		}
		return domain.ErrUnavailable.WithCause(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return domain.ErrUnavailable.WithCause(err)
	}
	switch {
	case len(body) > maxBody:
		return domain.Refused("the answer is over 16MB; aggregate the query or narrow its range")
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity:
		return domain.Refused(bad(body))
	case resp.StatusCode == http.StatusServiceUnavailable && strings.Contains(string(body), "timeout"):
		return domain.Refused(fmt.Sprintf("the query ran past %s; narrow it", domain.QueryTimeout))
	case resp.StatusCode != http.StatusOK:
		return domain.ErrUnavailable.WithCause(fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body[:min(len(body), 300)]))))
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return domain.ErrUnavailable.WithCause(fmt.Errorf("read the answer: %w", err))
	}
	return nil
}

// sample is a [time, "value"] pair of both APIs; Loki's time is a string
// of nanoseconds in a log stream, a number of seconds elsewhere.
type sample [2]json.RawMessage

func (p sample) point() (domain.Point, error) {
	var secs float64
	if err := json.Unmarshal(p[0], &secs); err != nil {
		return domain.Point{}, fmt.Errorf("a point's time: %w", err)
	}
	var raw string
	if err := json.Unmarshal(p[1], &raw); err != nil {
		return domain.Point{}, fmt.Errorf("a point's value: %w", err)
	}
	at := time.Unix(0, int64(secs*1e9)).UTC().Truncate(time.Millisecond)
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return domain.Point{At: at}, nil //nolint:nilerr // NaN and the infinities are no value, not a broken answer.
	}
	return domain.Point{At: at, Value: &v}, nil
}

// result is the data of a query answer of both APIs.
type result struct {
	Type   string          `json:"resultType"` //nolint:tagliatelle // Prometheus and Loki name it so.
	Result json.RawMessage `json:"result"`
}

// seriesOf reads a vector, a matrix or a scalar.
func seriesOf(r result) ([]domain.Series, error) {
	switch r.Type {
	case "scalar":
		var p sample
		if err := json.Unmarshal(r.Result, &p); err != nil {
			return nil, fmt.Errorf("a scalar: %w", err)
		}
		pt, err := p.point()
		return []domain.Series{{Labels: map[string]string{}, Points: []domain.Point{pt}}}, err
	case "vector", "matrix":
		var rows []struct {
			Metric map[string]string `json:"metric"`
			Value  *sample           `json:"value"`
			Values []sample          `json:"values"`
		}
		if err := json.Unmarshal(r.Result, &rows); err != nil {
			return nil, fmt.Errorf("a %s: %w", r.Type, err)
		}
		out := make([]domain.Series, 0, len(rows))
		for _, row := range rows {
			samples := row.Values
			if row.Value != nil {
				samples = []sample{*row.Value}
			}
			s := domain.Series{Labels: row.Metric, Points: make([]domain.Point, 0, len(samples))}
			if s.Labels == nil {
				s.Labels = map[string]string{}
			}
			for _, p := range samples {
				pt, err := p.point()
				if err != nil {
					return nil, err
				}
				s.Points = append(s.Points, pt)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("a result of type %q", r.Type)
	}
}

func seconds(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', 3, 64)
}
