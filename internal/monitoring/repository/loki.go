package repository

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

// Loki reads the logs through Loki's HTTP API at base, such as
// http://lgtm:3100; an empty base has none.
type Loki struct{ src source }

func NewLoki(base string, client *http.Client) *Loki {
	return &Loki{src: source{base: base, http: client}}
}

// noise are the fields every OpenTelemetry log line carries that tell the
// reader nothing.
var noise = []string{"observed_timestamp", "flags", "severity_number", "scope_name", "detected_level", "service_version", "deployment_environment_name"}

func lokiBad(body []byte) string {
	var a struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &a) == nil && a.Message != "" {
		return a.Message
	}
	return strings.TrimSpace(string(body[:min(len(body), 500)]))
}

// Query asks for one line more than limit, so the service can tell a cut.
// A span at one time runs a metric query there, as Loki's instant query.
func (l *Loki) Query(ctx context.Context, query string, span domain.Span, limit int) (Logged, error) {
	params := url.Values{"query": {query}, "limit": {strconv.Itoa(limit + 1)}, "direction": {"backward"}}
	path := "/loki/api/v1/query"
	if span.Range == 0 {
		params.Set("time", strconv.FormatInt(span.End.UnixNano(), 10))
	} else {
		path = "/loki/api/v1/query_range"
		params.Set("start", strconv.FormatInt(span.Start().UnixNano(), 10))
		params.Set("end", strconv.FormatInt(span.End.UnixNano(), 10))
		params.Set("step", strconv.FormatFloat(span.Step.Seconds(), 'f', -1, 64))
	}
	var a struct {
		Data result `json:"data"`
	}
	if err := l.src.get(ctx, path, params, &a, lokiBad); err != nil {
		return Logged{}, err
	}
	if a.Data.Type != "streams" {
		s, err := seriesOf(a.Data)
		if err != nil {
			return Logged{}, domain.ErrUnavailable.WithCause(err)
		}
		return Logged{Series: s}, nil
	}
	var streams []struct {
		Stream map[string]string   `json:"stream"`
		Values [][]json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(a.Data.Result, &streams); err != nil {
		return Logged{}, domain.ErrUnavailable.WithCause(fmt.Errorf("read the streams: %w", err))
	}
	var lines []domain.LogLine
	for _, st := range streams {
		fields := make(map[string]string, len(st.Stream))
		for k, v := range st.Stream {
			if !slices.Contains(noise, k) {
				fields[k] = v
			}
		}
		for _, v := range st.Values {
			if len(v) < 2 {
				continue
			}
			var ns, line string
			if json.Unmarshal(v[0], &ns) != nil || json.Unmarshal(v[1], &line) != nil {
				return Logged{}, domain.ErrUnavailable.WithCause(fmt.Errorf("a log line of %s", st.Stream))
			}
			n, err := strconv.ParseInt(ns, 10, 64)
			if err != nil {
				return Logged{}, domain.ErrUnavailable.WithCause(fmt.Errorf("a log line's time %q: %w", ns, err))
			}
			lines = append(lines, domain.LogLine{At: time.Unix(0, n).UTC(), Fields: fields, Line: line})
		}
	}
	slices.SortStableFunc(lines, func(a, b domain.LogLine) int { return cmp.Compare(b.At.UnixNano(), a.At.UnixNano()) })
	return Logged{Lines: lines}, nil
}
