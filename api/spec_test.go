package api_test

import (
	"strings"
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

// The listing endpoints take ListingInput; ListingDraft is the draft
// resource, which ListingDraftInput writes.
func TestListingSchemaNames(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, method, want string }{
		{"/v1/listings", "POST", "#/ListingInput"},
		{"/v1/listings/{id}", "PUT", "#/ListingInput"},
		{"/v1/listing-drafts/{id}", "PUT", "#/ListingDraftInput"},
	} {
		item := doc.Paths.Find(c.path)
		if item == nil || item.GetOperation(c.method) == nil {
			t.Errorf("%s %s is not in the spec", c.method, c.path)
			continue
		}
		ref := item.GetOperation(c.method).RequestBody.Value.Content.Get("application/json").Schema.Ref
		if !strings.HasSuffix(ref, c.want) {
			t.Errorf("%s %s takes %s, want %s", c.method, c.path, ref, c.want)
		}
	}
	if t.Failed() {
		return
	}
	got := doc.Paths.Find("/v1/listing-drafts/{id}").Put.Responses.Status(200).Value.Content.Get("application/json").Schema.Ref
	if !strings.HasSuffix(got, "#/ListingDraft") {
		t.Errorf("PUT /v1/listing-drafts/{id} answers %s", got)
	}
}
