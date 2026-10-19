package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server2/internal/platform/config"
)

func TestHealthz(t *testing.T) {
	a, err := New(context.Background(), config.Config{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close(context.Background())

	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || string(body) != `{"status":"ok"}`+"\n" {
		t.Fatalf("got %d %q", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
}
