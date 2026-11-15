package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/account/handler"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// deletes records which session delete ran; the other methods of
// service.Service are nil and panic when called.
type deletes struct {
	service.Service

	called string
	id     uuid.UUID
}

func (d *deletes) DeleteCurrentSession(context.Context, authn.Principal) error {
	d.called = "DeleteCurrentSession"
	return nil
}

func (d *deletes) DeleteAllSessions(context.Context, authn.Principal) error {
	d.called = "DeleteAllSessions"
	return nil
}

func (d *deletes) DeleteSession(_ context.Context, _ authn.Principal, id uuid.UUID) error {
	d.called, d.id = "DeleteSession", id
	return nil
}

// TestCurrentSessionRoute sends DELETE /v1/auth/sessions/current through the
// request validator of api/openapi.yaml and the router: both must take it as
// its own operation. Were either to match {id}, "current" would fail the uuid
// format with 400 or reach DeleteSession.
func TestCurrentSessionRoute(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	served := httpx.NewRouter()
	handler.New(&deletes{}, discard).Register(served, func(h http.Handler) http.Handler { return h })
	validate, err := httpx.ValidateRequests(doc, served.Routes(), discard)
	if err != nil {
		t.Fatal(err)
	}
	signedIn := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := authn.Principal{AccountID: uuid.NewV7(), SessionID: uuid.NewV7(), AuthTime: now}
			next.ServeHTTP(w, r.WithContext(authn.With(r.Context(), p)))
		})
	}
	id := uuid.NewV7()
	for _, c := range []struct {
		method, path string
		status       int
		code, called string
	}{
		{"DELETE", "/v1/auth/sessions/current", 204, "", "DeleteCurrentSession"},
		{"DELETE", "/v1/auth/sessions", 204, "", "DeleteAllSessions"},
		{"DELETE", "/v1/auth/sessions/" + id.String(), 204, "", "DeleteSession"},
		{"DELETE", "/v1/auth/sessions/currently", 400, "invalid_request", ""},
		{"DELETE", "/v1/auth/sessions/", 404, "", ""}, // an empty id signs nothing out
		{"POST", "/v1/auth/logout", 404, "", ""},
		{"POST", "/v1/auth/logout-all", 404, "", ""},
	} {
		svc := &deletes{}
		r := httpx.NewRouter()
		handler.New(svc, discard).Register(r, signedIn)
		req := httptest.NewRequestWithContext(t.Context(), c.method, c.path, http.NoBody)
		rec := httptest.NewRecorder()
		validate(r).ServeHTTP(rec, req)
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != c.status || body.Error.Code != c.code || svc.called != c.called {
			t.Errorf("%s %s: %d %q, called %q", c.method, c.path, rec.Code, body.Error.Code, svc.called)
		}
		if c.called == "DeleteSession" && svc.id != id {
			t.Errorf("%s %s: id %s", c.method, c.path, svc.id)
		}
	}
}
