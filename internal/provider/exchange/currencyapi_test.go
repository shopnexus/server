package exchange_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shopnexus-server/internal/provider/exchange"
)

func TestCurrencyAPI_FetchLatest_Success(t *testing.T) {
	body := `{
		"meta": {"last_updated_at": "2026-04-20T17:38:00Z"},
		"data": {
			"VND": {"code": "VND", "value": 25000.5},
			"JPY": {"code": "JPY", "value": 155.2}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("base_currency") != "USD" {
			t.Fatalf("expected base_currency=USD, got %q", r.URL.Query().Get("base_currency"))
		}
		if r.URL.Query().Get("apikey") != "test-key" {
			t.Fatalf("apikey not forwarded, got %q", r.URL.Query().Get("apikey"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := exchange.NewCurrencyAPI(srv.URL, "test-key", &http.Client{Timeout: 2 * time.Second})
	snap, err := client.FetchLatest(context.Background(), "USD", []string{"VND", "JPY"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Base != "USD" {
		t.Errorf("Base = %q, want USD", snap.Base)
	}
	if snap.Rates["VND"] != 25000.5 || snap.Rates["JPY"] != 155.2 {
		t.Errorf("rates = %+v", snap.Rates)
	}
}

func TestCurrencyAPI_FetchLatest_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := exchange.NewCurrencyAPI(srv.URL, "bad-key", &http.Client{Timeout: 1 * time.Second})
	_, err := client.FetchLatest(context.Background(), "USD", []string{"VND"})
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
}

func TestCurrencyAPI_FetchLatest_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	client := exchange.NewCurrencyAPI(srv.URL, "k", &http.Client{Timeout: 1 * time.Second})
	_, err := client.FetchLatest(context.Background(), "USD", []string{"VND"})
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}
