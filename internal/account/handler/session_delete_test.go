package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/shopnexus/server/api"
	"github.com/shopnexus/server/internal/account/handler"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
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

func (d *deletes) DeleteOtherSessions(context.Context, authn.Principal) error {
	d.called = "DeleteOtherSessions"
	return nil
}

func (d *deletes) DeleteSession(_ context.Context, _ authn.Principal, id uuid.UUID) error {
	d.called, d.id = "DeleteSession", id
	return nil
}

// TestFixedSessionRoutes sends DELETE /v1/auth/sessions/current and
// /v1/auth/sessions/others through the request validator of
// api/openapi.yaml and the router: both must take each as its own
// operation. Were either to match {id}, "current" or "others" would fail the
// uuid format with 400 or reach DeleteSession.
func TestFixedSessionRoutes(t *testing.T) {
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
		{"DELETE", "/v1/auth/sessions/others", 204, "", "DeleteOtherSessions"},
		{"DELETE", "/v1/auth/sessions/" + id.String(), 204, "", "DeleteSession"},
		{"DELETE", "/v1/auth/sessions/currently", 400, "invalid_request", ""},
		// The validator reads both as {id} with an empty id, which signs
		// nothing out; the first was the sign-out of every session until
		// 2026-11-17.
		{"DELETE", "/v1/auth/sessions", 400, "invalid_request", ""},
		{"DELETE", "/v1/auth/sessions/", 400, "invalid_request", ""},
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
