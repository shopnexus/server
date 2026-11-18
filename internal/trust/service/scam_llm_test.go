package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/provider/llm"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/service"
)

// The classifier sends one line per message, marked with its side, under a
// strict schema whose enum is the message codes, and decodes the answer.
func TestLLMClassifier(t *testing.T) {
	t.Parallel()
	var user string
	var enum []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			ResponseFormat struct {
				JSONSchema struct {
					Schema struct {
						Properties struct {
							A struct {
								Items struct {
									Enum []string `json:"enum"`
								} `json:"items"`
							} `json:"a"`
						} `json:"properties"`
					} `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Messages) != 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		user, enum = body.Messages[1].Content, body.ResponseFormat.JSONSchema.Schema.Properties.A.Items.Enum
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"a\":[],\"b\":[\"prepayment_request\"]}"}}]}`))
	}))
	defer srv.Close()
	c := service.LLMClassifier{
		Model: llm.OpenAI{BaseURL: srv.URL, APIKey: "k", Model: "scam-check", HTTP: srv.Client()},
		Alias: "scam-check", Timeout: time.Second, Log: slog.New(slog.DiscardHandler),
	}
	v, err := c.Classify(context.Background(), domain.Window{Lines: []domain.Line{
		{Side: domain.SideA, Text: "Còn không bạn?"},
		{Side: domain.SideB, Text: "Còn, chuyển cọc\ntrước nhé"},
	}})
	if err != nil || len(v.A) != 0 || len(v.B) != 1 || v.B[0] != domain.CodePrepayment {
		t.Fatalf("%+v %v", v, err)
	}
	if user != "A: Còn không bạn?\nB: Còn, chuyển cọc trước nhé\n" {
		t.Fatalf("user %q", user)
	}
	want := make([]string, len(domain.MessageCodes))
	for i, code := range domain.MessageCodes {
		want[i] = string(code)
	}
	if !slices.Equal(enum, want) {
		t.Fatalf("enum %v, want %v", enum, want)
	}
	if c.Name() != "llm:scam-check" || c.PromptVersion() != service.ScamPromptVersion {
		t.Fatalf("%s %d", c.Name(), c.PromptVersion())
	}
	if !strings.Contains(service.ScamSystemPrompt, "prepayment_request") {
		t.Fatal("the prompt does not explain the codes")
	}
}

func TestLLMClassifierRefused(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "content policy", http.StatusBadRequest)
	}))
	defer srv.Close()
	c := service.LLMClassifier{
		Model: llm.OpenAI{BaseURL: srv.URL, APIKey: "k", Model: "m", HTTP: srv.Client()},
		Alias: "m", Timeout: time.Second, Log: slog.New(slog.DiscardHandler),
	}
	if _, err := c.Classify(context.Background(), domain.Window{}); !errors.Is(err, service.ErrRefused) {
		t.Fatalf("%v", err)
	}
}
