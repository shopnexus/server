package app

import (
	"slices"
	"testing"

	"github.com/shopnexus/server/api"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// TestRoutesMatchSpec holds the registered routes and the OpenAPI operations
// to the same set: an operation without a route is unbuilt, and a route
// without an operation is undocumented.
func TestRoutesMatchSpec(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	var spec []httpx.Route
	for r := range httpx.Operations(doc) {
		spec = append(spec, r)
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
