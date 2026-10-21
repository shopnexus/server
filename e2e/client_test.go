package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/shopnexus/server2/api"
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
	specRouter, err = legacy.NewRouter(doc)
	return err
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

// Response is what a call returned.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// call sends a request to base+path, fails the test when the response does
// not match the operation in the spec (status included), and records the
// status for the coverage check in TestMain.
func call(t *testing.T, base, method, path string, body any, headers ...[2]string) Response {
	t.Helper()
	var reqBody []byte
	if body != nil {
		var err error
		if reqBody, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, base+path, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}

	resp, err := http.DefaultClient.Do(req)
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

	return Response{Status: resp.StatusCode, Header: resp.Header, Body: respBody}
}
