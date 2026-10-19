package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// TestRoutesMatchSpec holds the registered routes and the OpenAPI operations
// to the same set: an operation without a route is unbuilt, and a route
// without an operation is undocumented.
func TestRoutesMatchSpec(t *testing.T) {
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	var spec []httpx.Route
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			spec = append(spec, httpx.Route{Method: strings.ToUpper(method), Path: path})
		}
	}

	registered := newApp(t).Routes()

	for _, r := range spec {
		if !slices.Contains(registered, r) {
			t.Errorf("operation %s %s has no route", r.Method, r.Path)
		}
	}
	for _, r := range registered {
		if !slices.Contains(spec, r) {
			t.Errorf("route %s %s is not in api/openapi.yaml", r.Method, r.Path)
		}
	}
}
