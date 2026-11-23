package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/shopnexus/server/api"
)

var (
	specRouter routers.Router

	mu   sync.Mutex
	seen = map[string]map[int]bool{} // operationId → status → observed
)

func loadSpec() error {
	doc, err := api.Load()
	if err != nil {
		return err
	}
	doc.Servers = nil // match paths on any host, the test server included
	openapi3filter.RegisterBodyDecoder("text/event-stream", decodeEvents)
	specRouter, err = legacy.NewRouter(doc)
	return err
}

// decodeEvents checks the data of every server-sent event in body against
// the response's schema and gives the last one to the validator.
func decodeEvents(body io.Reader, _ http.Header, schema *openapi3.SchemaRef, _ openapi3filter.EncodingFn) (any, error) {
	var last any
	lines := bufio.NewScanner(body)
	for lines.Scan() {
		data, ok := strings.CutPrefix(lines.Text(), "data: ")
		if !ok {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil, fmt.Errorf("event data %q: %w", data, err)
		}
		if err := schema.Value.VisitJSON(v); err != nil {
			return nil, fmt.Errorf("event data %q: %w", data, err)
		}
		last = v
	}
	if last == nil {
		return nil, errors.New("a stream without events")
	}
	return last, nil
}

// record notes that op answered status, for the coverage check.
func record(op string, status int) {
	mu.Lock()
	defer mu.Unlock()
	if seen[op] == nil {
		seen[op] = map[int]bool{}
	}
	seen[op][status] = true
}

func observed(op string, status int) bool {
	mu.Lock()
	defer mu.Unlock()
	return seen[op][status]
}

// client returns redirects as they are, so a test reads their status and
// Location.
var client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// Response is what a call returned, with the request it answered.
type Response struct {
	Method string
	Path   string
	Status int
	Header http.Header
	Body   []byte
}

// form is a body sent form-encoded, as GHTK posts its reports.
type form url.Values

// raw is a body sent as it is, under contentType.
type raw struct{ contentType, body string }

// call sends a request to the shared server; see callAt.
func call(t *testing.T, method, path string, body any, headers ...[2]string) Response {
	t.Helper()
	return callAt(t, server, method, path, body, headers...)
}

// callAt sends a request to base+path, fails the test when the response
// does not match the operation in the spec (status included), and records
// the status for the coverage check in TestMain. A "Host" header sets the
// request's host, and a header with no name is left out.
func callAt(t *testing.T, base, method, path string, body any, headers ...[2]string) Response {
	t.Helper()
	var reqBody []byte
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case form:
		reqBody, contentType = []byte(url.Values(b).Encode()), "application/x-www-form-urlencoded"
	case raw:
		reqBody, contentType = []byte(b.body), b.contentType
	default:
		var err error
		if reqBody, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), method, base+path, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-Forwarded-For", testAddress(t))
	for _, h := range headers {
		switch h[0] {
		case "": // bearer("") for a call signed out
		case "Host":
			req.Host = h[1]
		default:
			req.Header.Set(h[0], h[1])
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	route, params, err := specRouter.FindRoute(req)
	if err != nil {
		t.Fatalf("%s %s is not in the spec: %v", method, path, err)
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request: req, PathParams: params, Route: route,
			Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		},
		Status:  resp.StatusCode,
		Header:  resp.Header,
		Body:    io.NopCloser(bytes.NewReader(respBody)),
		Options: &openapi3filter.Options{IncludeResponseStatus: true, MultiError: true},
	}
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		t.Fatalf("%s %s → %d does not match the spec: %v\nbody: %s", method, path, resp.StatusCode, err, respBody)
	}

	record(route.Operation.OperationID, resp.StatusCode)

	return Response{Method: method, Path: path, Status: resp.StatusCode, Header: resp.Header, Body: respBody}
}

// testAddress is t's own client address, an IPv6 /64 hashed from its name,
// so one test's calls fill only its own rate limit buckets.
func testAddress(t *testing.T) string {
	h := fnv.New64a()
	h.Write([]byte(t.Name()))
	var a [16]byte
	binary.BigEndian.PutUint64(a[:8], h.Sum64())
	a[0], a[15] = 0xfd, 1
	return netip.AddrFrom16(a).String()
}
