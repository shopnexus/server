package exchange_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shopnexus-server/internal/provider/exchange"
)

func TestFrankfurter_FetchLatest_Success(t *testing.T) {
	body := `{"amount":1.0,"base":"USD","date":"2026-04-20","rates":{"VND":25000.5,"JPY":155.2}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("base") != "USD" {
			t.Fatalf("expected base=USD, got %q", r.URL.Query().Get("base"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := exchange.NewFrankfurter(srv.URL, &http.Client{Timeout: 2 * time.Second})
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

func TestFrankfurter_FetchLatest_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := exchange.NewFrankfurter(srv.URL, &http.Client{Timeout: 1 * time.Second})
	_, err := client.FetchLatest(context.Background(), "USD", []string{"VND"})
	if err == nil {
		t.Fatal("expected error on 5xx, got nil")
	}
}

func TestFrankfurter_FetchLatest_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	client := exchange.NewFrankfurter(srv.URL, &http.Client{Timeout: 1 * time.Second})
	_, err := client.FetchLatest(context.Background(), "USD", []string{"VND"})
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}
