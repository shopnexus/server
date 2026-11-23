package api_test

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/shopnexus/server/api"
	"github.com/shopnexus/server/internal/platform/httpx"
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

// The listing endpoints take ListingInput; ListingDraft is the draft
// resource, which ListingDraftInput writes.
func TestSpecHasTheSupportRoutes(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	for path, ops := range map[string][]string{
		"/v1/support/tickets":                       {"GET", "POST"},
		"/v1/support/summary":                       {"GET"},
		"/v1/support/orders/{order_id}/ticket":      {"GET"},
		"/v1/console/support/tickets":               {"GET"},
		"/v1/console/support/tickets/{id}":          {"GET"},
		"/v1/console/support/tickets/{id}/messages": {"GET"},
		"/v1/console/support/tickets/{id}/replies":  {"POST"},
		"/v1/console/support/tickets/{id}/resolve":  {"POST"},
		"/v1/console/support/tickets/{id}/reopen":   {"POST"},
		"/v1/console/support/summary":               {"GET"},
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
	for _, gone := range []string{"/v1/feedback", "/v1/console/feedback"} {
		if doc.Paths.Find(gone) != nil {
			t.Errorf("%s should be gone", gone)
		}
	}
	draft := doc.Paths.Find("/v1/conversations/{id}/messages").Post.RequestBody.Value.Content.Get("application/json").Schema.Value
	if draft.Properties["reply_to"] == nil {
		t.Error("MessageDraft needs reply_to")
	}
	message := doc.Paths.Find("/v1/conversations/{id}/messages").Post.Responses.Status(201).Value.Content.Get("application/json").Schema.Value
	if message.Properties["reply_to"] == nil {
		t.Error("Message needs reply_to")
	}
}

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

// An operation names the use cases it serves in x-uc, by their IDs in the
// docs repository's registry/requirements.yaml. `make trace` in the
// umbrella repository checks that the IDs exist; this checks their form.
func TestSpecUseCaseIDs(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	id := regexp.MustCompile(`^UC-[A-Z]+-\d{2}$`)
	tagged := 0
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			raw, ok := op.Extensions["x-uc"]
			if !ok {
				continue
			}
			tagged++
			ucs, ok := raw.([]any)
			if !ok || len(ucs) == 0 {
				t.Errorf("%s %s: x-uc is not a list of IDs: %v", method, path, raw)
				continue
			}
			for _, uc := range ucs {
				if s, isString := uc.(string); !isString || !id.MatchString(s) {
					t.Errorf("%s %s: x-uc %v is not a UC ID", method, path, uc)
				}
			}
		}
	}
	if tagged == 0 {
		t.Error("no operation has x-uc")
	}
}

// x-rate-limit names a tier of httpx. An exempt operation counts nothing,
// so adding one means adding it here too.
func TestSpecRateLimitTiers(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	exempt := []string{"getHealthz", "getReadyz", "carrierWebhook", "authorizeConsoleDashboard"}
	var got []string
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			raw, ok := op.Extensions["x-rate-limit"]
			if !ok {
				continue
			}
			name, _ := raw.(string)
			if _, ok := httpx.TierNamed(name); !ok {
				t.Errorf("%s %s: x-rate-limit %v is not a tier", method, path, raw)
			}
			if name == httpx.Exempt.Name {
				got = append(got, op.OperationID)
			}
		}
	}
	slices.Sort(got)
	slices.Sort(exempt)
	if !slices.Equal(got, exempt) {
		t.Errorf("exempt operations %v, want %v", got, exempt)
	}
}

// An operation has one tag of x-tagGroups, its section in the reference.
// internal and websocket are the one list of routes the app's generated
// client leaves out: internal on the console, the dev routes, the
// dashboard callback, the carrier webhook, the health checks and the
// website's sitemap, websocket on /v1/ws, and neither on any other
// operation.
func TestSpecTags(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	var groups []struct {
		Tags []string `json:"tags"`
	}
	raw, err := json.Marshal(doc.Extensions["x-tagGroups"])
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &groups); err != nil {
		t.Fatal(err)
	}
	sections := map[string]bool{}
	for _, g := range groups {
		for _, tag := range g.Tags {
			sections[tag] = true
		}
	}
	if len(sections) == 0 {
		t.Fatal("x-tagGroups names no tag")
	}
	internal := []string{"/_dashboard/callback", "/v1/carriers/{carrier}/webhook/{secret}", "/healthz", "/readyz"}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			inSection := 0
			for _, tag := range op.Tags {
				if doc.Tags.Get(tag) == nil {
					t.Errorf("%s %s: tag %s is not declared", method, path, tag)
				}
				if sections[tag] {
					inSection++
				} else if tag != "internal" && tag != "websocket" {
					t.Errorf("%s %s: tag %s is in no group of x-tagGroups", method, path, tag)
				}
			}
			if inSection != 1 {
				t.Errorf("%s %s: %d tags of x-tagGroups, want 1", method, path, inSection)
			}
			var want []string
			switch {
			case strings.HasPrefix(path, "/v1/console/"), strings.HasPrefix(path, "/v1/dev/"), strings.HasPrefix(path, "/v1/sitemap"),
				slices.Contains(internal, path):
				want = []string{"internal"}
			case path == "/v1/ws":
				want = []string{"websocket"}
			default:
			}
			got := slices.DeleteFunc(slices.Clone(op.Tags), func(tag string) bool { return tag != "internal" && tag != "websocket" })
			if !slices.Equal(got, want) {
				t.Errorf("%s %s: tagged %v of internal and websocket, want %v", method, path, got, want)
			}
		}
	}
	for _, path := range append(internal, "/v1/ws") {
		if doc.Paths.Find(path) == nil {
			t.Errorf("%s is not in the spec", path)
		}
	}
}
