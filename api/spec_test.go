package api_test

import (
	"slices"
	"testing"

	"github.com/shopnexus/server2/api"
)

func TestSpecValid(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Paths.Find("/healthz") == nil || doc.Paths.Find("/healthz").Get == nil {
		t.Fatal("/healthz GET missing after resolving $refs")
	}
}

func TestSpecHasTheCarrierRoutes(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	for path, ops := range map[string][]string{
		"/v1/carrier-accounts":                    {"GET"},
		"/v1/carrier-accounts/{carrier}":          {"PUT", "DELETE"},
		"/v1/carrier-accounts/{carrier}/pickups":  {"GET"},
		"/v1/shipments/quote":                     {"GET"},
		"/v1/carriers/{carrier}/webhook/{secret}": {"POST"},
	} {
		item := doc.Paths.Find(path)
		if item == nil {
			t.Errorf("%s missing", path)
			continue
		}
		for _, op := range ops {
			if item.GetOperation(op) == nil {
				t.Errorf("%s %s missing", op, path)
			}
		}
	}
	draft := doc.Paths.Find("/v1/orders/{order_id}/shipment").Post.RequestBody.Value.Content.Get("application/json").Schema.Value
	var linkedDraft bool
	for _, s := range draft.OneOf {
		if _, ok := s.Value.Properties["pickup_from"]; ok {
			linkedDraft = true
			if !slices.Contains(s.Value.Required, "carrier") {
				t.Error("a linked draft does not require carrier")
			}
		}
	}
	if !linkedDraft {
		t.Error("no shipment draft has pickup_from")
	}
}
