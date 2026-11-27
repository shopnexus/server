package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// stubMonitoring is the stack's Prometheus and Loki on one server. A
// query holding "bad" is refused as Prometheus and Loki refuse a query
// they cannot parse, and one holding "down" answers 500. Every other
// metrics query is one series at 0.5 that carries the labels the health
// groups by; a log selector gives two lines, the newer holding an email,
// a log sum 1000 requests, and another log metric query the visitors' counts.
type stubMonitoring struct{ srv *httptest.Server }

func newMonitoring() *stubMonitoring {
	m := &stubMonitoring{}
	mux := http.NewServeMux()
	mux.HandleFunc("/prometheus/api/v1/query", m.metrics)
	mux.HandleFunc("/prometheus/api/v1/query_range", m.metrics)
	mux.HandleFunc("/prometheus/api/v1/label/__name__/values", func(w http.ResponseWriter, _ *http.Request) {
		answer(w, `["http_server_request_duration_seconds_bucket","http_server_request_duration_seconds_count","up"]`)
	})
	mux.HandleFunc("/prometheus/api/v1/metadata", func(w http.ResponseWriter, _ *http.Request) {
		answer(w, `{"http_server_request_duration_seconds":[{"type":"histogram","help":"Duration of HTTP server requests.","unit":"s"}]}`)
	})
	mux.HandleFunc("/prometheus/api/v1/labels", func(w http.ResponseWriter, _ *http.Request) {
		answer(w, `["__name__","http_route","job"]`)
	})
	mux.HandleFunc("/loki/loki/api/v1/query", m.logs)
	mux.HandleFunc("/loki/loki/api/v1/query_range", m.logs)
	m.srv = httptest.NewServer(mux)
	return m
}

func (m *stubMonitoring) metricsURL() string { return m.srv.URL + "/prometheus" }
func (m *stubMonitoring) logsURL() string    { return m.srv.URL + "/loki" }

func answer(w http.ResponseWriter, data string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"status":"success","data":%s}`, data)
}

// refused answers a query the source would not run, or 500 for one
// holding "down"; it reports whether it did.
func refused(w http.ResponseWriter, q string) bool {
	switch {
	case strings.Contains(q, "down"):
		http.Error(w, "boom", http.StatusInternalServerError)
	case strings.Contains(q, "bad"):
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"1:4: parse error: unexpected identifier \"bad\""}`))
	default:
		return false
	}
	return true
}

func (*stubMonitoring) metrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("query")
	if refused(w, q) {
		return
	}
	labels := `{"http_route":"/v1/search","server_address":"bifrost","restate_service":"Orders","restate_handler":"Pay","service_instance_id":"server-1"}`
	at := time.Now().Unix()
	if r.URL.Path == "/prometheus/api/v1/query_range" {
		answer(w, fmt.Sprintf(`{"resultType":"matrix","result":[{"metric":%s,"values":[[%d,"0.5"],[%d,"NaN"]]}]}`, labels, at-60, at))
		return
	}
	answer(w, fmt.Sprintf(`{"resultType":"vector","result":[{"metric":%s,"value":[%d,"0.5"]}]}`, labels, at))
}

func (*stubMonitoring) logs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("query")
	if refused(w, q) {
		return
	}
	at := time.Now().UnixNano()
	if strings.HasPrefix(q, "{") {
		answer(w, fmt.Sprintf(`{"resultType":"streams","result":[{"stream":{"route":"/v1/me","status":"500","flags":"1"},`+
			`"values":[["%d","mail to lan@example.vn failed"],["%d","request"]]}]}`, at, at-int64(time.Second)))
		return
	}
	if strings.HasPrefix(q, "sum(") {
		answer(w, fmt.Sprintf(`{"resultType":"vector","result":[{"metric":{},"value":[%d,"1000"]}]}`, at/1e9))
		return
	}
	answer(w, fmt.Sprintf(`{"resultType":"vector","result":[`+
		`{"metric":{"visitor":"g:4f1c2d3e4a5b","client":"other","country":"SG"},"value":[%d,"900"]},`+
		`{"metric":{"visitor":"a:0193e100","client":"android","country":"VN"},"value":[%d,"100"]}]}`, at/1e9, at/1e9))
}
