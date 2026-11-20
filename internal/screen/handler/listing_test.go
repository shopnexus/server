package handler_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/shopnexus/server/api"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/handler"
	"github.com/shopnexus/server/internal/screen/service"
)

// listings records the reads the handler asks of the service; the other
// methods of service.Service are nil and panic when called.
type listings struct {
	service.Service

	reads []service.ListingRead
}

func (l *listings) Listing(_ context.Context, _ *uuid.UUID, in service.ListingRead) (service.ListingScreen, error) {
	l.reads = append(l.reads, in)
	return service.ListingScreen{}, nil
}

// TestListingPrefetch sends GET /v1/screens/listing/{id} through the
// request validator of api/openapi.yaml and the router: Sec-Purpose passes
// the spec and reaches the service as Prefetch.
func TestListingPrefetch(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	discard := slog.New(slog.DiscardHandler)
	pass := func(h http.Handler) http.Handler { return h }
	for purpose, want := range map[string]bool{"": false, "prefetch": true, "prefetch;prerender": true} {
		svc := &listings{}
		r := httpx.NewRouter()
		handler.New(svc, discard).Register(r, pass, pass)
		validate, err := httpx.ValidateRequests(doc, r.Routes(), discard)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/screens/listing/ghe-go-abcd1234", http.NoBody)
		if purpose != "" {
			req.Header.Set("Sec-Purpose", purpose)
		}
		rec := httptest.NewRecorder()
		validate(r).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || len(svc.reads) != 1 {
			t.Fatalf("Sec-Purpose %q: %d, %d reads", purpose, rec.Code, len(svc.reads))
		}
		if got := svc.reads[0]; got.Ref != "ghe-go-abcd1234" || got.Prefetch != want {
			t.Errorf("Sec-Purpose %q: %+v", purpose, got)
		}
	}
}
