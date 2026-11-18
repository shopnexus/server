package llm_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/provider/llm"
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

// A request with images sends the user message as parts: the text, then
// one data URL per image, in order. A request without sends the string.
func TestCompleteSendsImages(t *testing.T) {
	t.Parallel()
	type part struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	var got []json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Messages) != 2 || body.Messages[1].Role != "user" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		got = append(got, body.Messages[1].Content)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer srv.Close()
	c := llm.OpenAI{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTP: srv.Client()}
	var out map[string]any
	one, two := []byte{0xFF, 0xD8, 0xFF, 1}, []byte{0xFF, 0xD8, 0xFF, 2}
	req := llm.Request{System: "s", User: "u", Images: [][]byte{one, two}, SchemaName: "x", Schema: json.RawMessage(`{}`)}
	if err := c.Complete(context.Background(), req, &out); err != nil {
		t.Fatal(err)
	}
	req.Images = nil
	if err := c.Complete(context.Background(), req, &out); err != nil {
		t.Fatal(err)
	}
	var parts []part
	if err := json.Unmarshal(got[0], &parts); err != nil || len(parts) != 3 {
		t.Fatalf("with images: %s %v", got[0], err)
	}
	if parts[0].Type != "text" || parts[0].Text != "u" {
		t.Fatalf("first part: %+v", parts[0])
	}
	for i, img := range [][]byte{one, two} {
		p := parts[i+1]
		want := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)
		if p.Type != "image_url" || p.ImageURL.URL != want {
			t.Fatalf("image %d: %+v", i, p)
		}
	}
	if !bytes.Equal(got[1], []byte(`"u"`)) {
		t.Fatalf("without images the content is the string: %s", got[1])
	}
}

// A 4xx other than 402 and 429 is ErrRefused; 402, 429 and 5xx are plain errors.
func TestCompleteRefused(t *testing.T) {
	t.Parallel()
	for status, refused := range map[int]bool{400: true, 402: false, 403: true, 413: true, 429: false, 500: false, 503: false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "no", status)
		}))
		c := llm.OpenAI{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTP: srv.Client()}
		var out map[string]any
		err := c.Complete(context.Background(), llm.Request{System: "s", User: "u", SchemaName: "x", Schema: json.RawMessage(`{}`)}, &out)
		srv.Close()
		if err == nil || errors.Is(err, llm.ErrRefused) != refused {
			t.Fatalf("status %d: %v, want refused %v", status, err, refused)
		}
	}
}
