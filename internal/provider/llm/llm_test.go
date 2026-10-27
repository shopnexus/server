package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/provider/llm"
)

func TestComplete(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model          string `json:"model"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" ||
			json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "m" || body.ResponseFormat.Type != "json_schema" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"a\":1}"}}]}`))
	}))
	defer srv.Close()
	c := llm.OpenAI{BaseURL: srv.URL + "/v1/", APIKey: "k", Model: "m", HTTP: srv.Client()}
	var out struct {
		A int `json:"a"`
	}
	req := llm.Request{System: "s", User: "u", SchemaName: "x", Schema: json.RawMessage(`{"type":"object"}`)}
	if err := c.Complete(context.Background(), req, &out); err != nil || out.A != 1 {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestCompleteFails(t *testing.T) {
	t.Parallel()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer down.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	req := llm.Request{System: "s", User: "u", SchemaName: "x", Schema: json.RawMessage(`{}`)}
	var out map[string]any
	if err := (llm.OpenAI{BaseURL: down.URL, APIKey: "k", Model: "m", HTTP: down.Client()}).Complete(context.Background(), req, &out); err == nil {
		t.Fatal("a 500 answered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := (llm.OpenAI{BaseURL: slow.URL, APIKey: "k", Model: "m", HTTP: slow.Client()}).Complete(ctx, req, &out); err == nil {
		t.Fatal("a call past its deadline answered")
	}
}
