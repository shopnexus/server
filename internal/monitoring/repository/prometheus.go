package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/shopnexus/server/internal/monitoring/domain"
)

// Prometheus reads the metrics through Prometheus's HTTP API at base, such
// as http://lgtm:9090; an empty base has none.
type Prometheus struct{ src source }

func NewPrometheus(base string, client *http.Client) *Prometheus {
	return &Prometheus{src: source{base: base, http: client}}
}

// promAnswer is the envelope of every Prometheus answer.
type promAnswer[T any] struct {
	Status string `json:"status"`
	Data   T      `json:"data"`
	Error  string `json:"error"`
}

func promBad(body []byte) string {
	var a promAnswer[json.RawMessage]
	if json.Unmarshal(body, &a) == nil && a.Error != "" {
		return a.Error
	}
	return string(body[:min(len(body), 500)])
}

func (p *Prometheus) Query(ctx context.Context, query string, span domain.Span) ([]domain.Series, error) {
	params := url.Values{"query": {query}, "timeout": {domain.QueryTimeout.String()}}
	path := "/api/v1/query"
	if span.Range == 0 {
		params.Set("time", seconds(span.End))
	} else {
		path = "/api/v1/query_range"
		params.Set("start", seconds(span.Start()))
		params.Set("end", seconds(span.End))
		params.Set("step", strconv.FormatFloat(span.Step.Seconds(), 'f', -1, 64))
	}
	var a promAnswer[result]
	if err := p.src.get(ctx, path, params, &a, promBad); err != nil {
		return nil, err
	}
	s, err := seriesOf(a.Data)
	if err != nil {
		return nil, domain.ErrUnavailable.WithCause(err)
	}
	return s, nil
}

func (p *Prometheus) Names(ctx context.Context) ([]string, error) {
	var a promAnswer[[]string]
	if err := p.src.get(ctx, "/api/v1/label/__name__/values", url.Values{}, &a, promBad); err != nil {
		return nil, err
	}
	slices.Sort(a.Data)
	return a.Data, nil
}

func (p *Prometheus) Metadata(ctx context.Context) (map[string]Meta, error) {
	var a promAnswer[map[string][]struct {
		Type string `json:"type"`
		Help string `json:"help"`
	}]
	if err := p.src.get(ctx, "/api/v1/metadata", url.Values{}, &a, promBad); err != nil {
		return nil, err
	}
	out := make(map[string]Meta, len(a.Data))
	for name, ms := range a.Data {
		if len(ms) > 0 {
			out[name] = Meta{Type: ms[0].Type, Help: ms[0].Help}
		}
	}
	return out, nil
}

func (p *Prometheus) Labels(ctx context.Context, metric string) ([]string, error) {
	var a promAnswer[[]string]
	if err := p.src.get(ctx, "/api/v1/labels", url.Values{"match[]": {metric}}, &a, promBad); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(a.Data, func(l string) bool { return l == "__name__" }), nil
}
