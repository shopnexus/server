package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server/internal/provider/llm"
)

func TestStream(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["stream"] != any(true) || body["response_format"] == nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"iPhone\"}}]}\n\n: keep-alive\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\" 11\\nMáy đẹp\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := llm.OpenAI{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTP: srv.Client()}
	var seen []string
	got, err := c.Stream(context.Background(), llm.Request{System: "s", User: "u", SchemaName: "x", Schema: json.RawMessage(`{}`)},
		func(s string) { seen = append(seen, s) })
	if err != nil || got != "iPhone 11\nMáy đẹp" || len(seen) != 2 || seen[0] != "iPhone" {
		t.Fatalf("%q %q %v", got, seen, err)
	}
}

func TestStreamCutShort(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"iPh\"}}]}\n\n"))
	}))
	defer srv.Close()
	c := llm.OpenAI{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	if _, err := c.Stream(context.Background(), llm.Request{}, func(string) {}); err == nil {
		t.Fatal("a stream without [DONE] passed")
	}
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"image too large for key sk-abcdef123456\nmore"}}`))
	}))
	defer refusing.Close()
	c.BaseURL = refusing.URL
	_, err := c.Stream(context.Background(), llm.Request{}, func(string) {})
	var apiErr *llm.APIError
	if !errors.Is(err, llm.ErrRefused) || !errors.As(err, &apiErr) || apiErr.Message != "image too large for key ***" {
		t.Fatalf("400: %v", err)
	}
}

// An error the stream sends midway is an *APIError with its message.
func TestStreamError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"{\"}}]}\n\n" +
			"data: {\"error\":{\"message\":\"upstream overloaded\"}}\n\n"))
	}))
	defer srv.Close()
	c := llm.OpenAI{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	_, err := c.Stream(context.Background(), llm.Request{}, func(string) {})
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "upstream overloaded" || errors.Is(err, llm.ErrRefused) {
		t.Fatalf("%v", err)
	}
}
