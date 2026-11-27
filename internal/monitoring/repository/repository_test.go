package repository_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
	"github.com/shopnexus/server/internal/monitoring/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// fake answers each path with a status and a body, and keeps the last
// query string it was sent.
func fake(t *testing.T, routes map[string]func(url.Values) (int, string)) (string, *url.Values) {
	t.Helper()
	var last url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		last = r.URL.Query()
		status, body := f(last)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &last
}

var end = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

func TestPrometheusQuery(t *testing.T) {
	t.Parallel()
	ok := func(body string) func(url.Values) (int, string) {
		return func(url.Values) (int, string) { return 200, body }
	}
	base, last := fake(t, map[string]func(url.Values) (int, string){
		"/api/v1/query": ok(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"http_route":"/v1/search"},"value":[1795744800,"0.25"]},{"metric":{},"value":[1795744800,"NaN"]}]}}`),
		"/api/v1/query_range": ok(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"job":"server"},"values":[[1795741200,"1"],[1795741215,"+Inf"]]}]}}`),
		"/api/v1/label/__name__/values": ok(`{"status":"success","data":["up","http_server_request_duration_seconds_count"]}`),
		"/api/v1/metadata":              ok(`{"status":"success","data":{"up":[{"type":"gauge","help":"Up.","unit":""}]}}`),
		"/api/v1/labels":                ok(`{"status":"success","data":["__name__","instance","job"]}`),
	})
	p := repository.NewPrometheus(base, http.DefaultClient)
	ctx := context.Background()

	got, err := p.Query(ctx, "x", domain.Span{End: end})
	if err != nil || len(got) != 2 || got[0].Labels["http_route"] != "/v1/search" || *got[0].Last() != 0.25 || got[1].Last() != nil {
		t.Fatalf("a vector: %+v %v", got, err)
	}
	if last.Get("time") != "1795773600.000" || last.Get("timeout") != "15s" {
		t.Errorf("sent %v", *last)
	}
	span, _ := domain.NewSpan(end, time.Hour, 0)
	got, err = p.Query(ctx, "x", span)
	if err != nil || len(got[0].Points) != 2 || got[0].Points[1].Value != nil || !got[0].Points[0].At.Equal(time.Unix(1795741200, 0)) {
		t.Fatalf("a matrix: %+v %v", got, err)
	}
	if last.Get("step") != "15" || last.Get("start") != "1795770000.000" {
		t.Errorf("sent %v", *last)
	}
	if names, err := p.Names(ctx); err != nil || names[0] != "http_server_request_duration_seconds_count" {
		t.Errorf("names %v %v", names, err)
	}
	if meta, err := p.Metadata(ctx); err != nil || meta["up"].Type != "gauge" {
		t.Errorf("metadata %v %v", meta, err)
	}
	if labels, err := p.Labels(ctx, "up"); err != nil || len(labels) != 2 || last.Get("match[]") != "up" {
		t.Errorf("labels %v %v", labels, err)
	}
}

// A query Prometheus refuses comes back in its words; a source that is
// down or absent is unavailable. UC-MOD-13 E1, E2.
func TestPrometheusRefusals(t *testing.T) {
	t.Parallel()
	base, _ := fake(t, map[string]func(url.Values) (int, string){
		"/api/v1/query": func(q url.Values) (int, string) {
			switch q.Get("query") {
			case "bad":
				return 400, `{"status":"error","errorType":"bad_data","error":"1:5: parse error: unexpected end of input"}`
			case "slow":
				return 503, `{"status":"error","errorType":"timeout","error":"query timed out in expression evaluation"}`
			default:
				return 500, `boom`
			}
		},
	})
	p := repository.NewPrometheus(base, http.DefaultClient)
	_, err := p.Query(context.Background(), "bad", domain.Span{End: end})
	if e := apperr.From(err); !errors.Is(err, domain.ErrQueryInvalid) || e.Message != "1:5: parse error: unexpected end of input" {
		t.Errorf("bad: %v", err)
	}
	if _, err := p.Query(context.Background(), "slow", domain.Span{End: end}); !errors.Is(err, domain.ErrQueryInvalid) {
		t.Errorf("slow: %v", err)
	}
	if _, err := p.Query(context.Background(), "x", domain.Span{End: end}); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("500: %v", err)
	}
	if _, err := repository.NewPrometheus("", http.DefaultClient).Names(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("none set up: %v", err)
	}
	if _, err := repository.NewPrometheus("http://127.0.0.1:1", http.DefaultClient).Names(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("down: %v", err)
	}
}

func TestLokiQuery(t *testing.T) {
	t.Parallel()
	base, last := fake(t, map[string]func(url.Values) (int, string){
		"/loki/api/v1/query_range": func(q url.Values) (int, string) {
			if q.Get("query") == "bad" {
				return 400, `{"message":"parse error at line 1, col 2: syntax error","status":"error"}`
			}
			return 200, `{"status":"success","data":{"resultType":"streams","result":[` +
				`{"stream":{"route":"/v1/search","status":"200","flags":"1"},"values":[["1795773590000000000","request"],["1795773500000000000","request"]]},` +
				`{"stream":{"route":"/v1/me","status":"500"},"values":[["1795773595000000000","request"]]}]}}`
		},
		"/loki/api/v1/query": func(url.Values) (int, string) {
			return 200, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"visitor":"g:ab"},"value":[1795773600,"42"]}]}}`
		},
	})
	l := repository.NewLoki(base, http.DefaultClient)
	span, _ := domain.NewSpan(end, time.Hour, 0)
	got, err := l.Query(context.Background(), `{service_name="server"}`, span, 10)
	if err != nil || len(got.Lines) != 3 || got.Lines[0].Fields["route"] != "/v1/me" || got.Lines[2].At.Unix() != 1795773500 {
		t.Fatalf("lines %+v %v", got, err)
	}
	if _, noisy := got.Lines[1].Fields["flags"]; noisy || last.Get("limit") != "11" || last.Get("direction") != "backward" {
		t.Errorf("fields %v sent %v", got.Lines[1].Fields, *last)
	}
	got, err = l.Query(context.Background(), `count_over_time({service_name="server"}[5m])`, domain.Span{End: end}, 1)
	if err != nil || len(got.Series) != 1 || *got.Series[0].Last() != 42 || last.Get("time") != "1795773600000000000" {
		t.Errorf("instant %+v %v", got, err)
	}
	_, err = l.Query(context.Background(), "bad", span, 10)
	if e := apperr.From(err); !errors.Is(err, domain.ErrQueryInvalid) || e.Message != "parse error at line 1, col 2: syntax error" {
		t.Errorf("bad: %v", err)
	}
}
