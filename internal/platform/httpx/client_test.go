package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

//nolint:paralleltest // The test installs the global tracer provider.
func TestTransportSpanCarriesTrace(t *testing.T) {
	rec := useRecorder(t)
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Traceparent")
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v1/orders/42", http.NoBody)
	res, err := NewClient(0).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	s := rec.Ended()[0]
	if s.SpanKind() != trace.SpanKindClient || s.Name() != "GET "+req.URL.Host {
		t.Fatalf("span %s %q", s.SpanKind(), s.Name())
	}
	if !strings.Contains(got, s.SpanContext().TraceID().String()) || !strings.Contains(got, s.SpanContext().SpanID().String()) {
		t.Fatalf("traceparent %q, span %s", got, s.SpanContext().SpanID())
	}
	if v, _ := attr(s.Attributes(), "http.response.status_code"); v.AsInt64() != http.StatusTeapot {
		t.Fatalf("status = %v", v)
	}
	if s.Status().Code != codes.Error {
		t.Fatalf("a 4xx reply marks the span: %v", s.Status())
	}
	if req.Header.Get("Traceparent") != "" {
		t.Fatal("the caller's request was changed")
	}
}

type failing struct{}

func (failing) RoundTrip(*http.Request) (*http.Response, error) { return nil, errors.New("refused") }

//nolint:paralleltest // The test installs the global tracer provider.
func TestTransportRecordsFailure(t *testing.T) {
	rec := useRecorder(t)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://carrier.test/orders", http.NoBody)
	if _, err := Transport(failing{}).RoundTrip(req); err == nil { //nolint:bodyclose // The transport fails; there is no body.
		t.Fatal("want the transport's error")
	}
	s := rec.Ended()[0]
	if v, _ := attr(s.Attributes(), "error.type"); v.AsString() != "transport" || s.Status().Code != codes.Error {
		t.Fatalf("error.type %v, status %v", v, s.Status())
	}
}
