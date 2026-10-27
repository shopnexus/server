// Package llm asks a language model for structured answers through an
// OpenAI-compatible chat completions endpoint. It knows nothing about what
// the answers are for; callers write the prompt and the schema.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Request is one question: the instructions, the text to read, and the
// JSON schema the answer must fit.
type Request struct {
	System, User string
	SchemaName   string
	Schema       json.RawMessage
}

// Client asks a model.
type Client interface {
	// Complete asks for an answer that fits r.Schema and decodes it into out.
	Complete(ctx context.Context, r Request, out any) error
}

// OpenAI is a Client for an OpenAI-compatible API, such as a proxy in front
// of several models.
type OpenAI struct {
	BaseURL, APIKey, Model string
	HTTP                   *http.Client
}

// maxReply caps how much of a reply is read.
const maxReply = 1 << 20

func (c OpenAI) Complete(ctx context.Context, r Request, out any) error {
	body, err := json.Marshal(map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": r.System},
			{"role": "user", "content": r.User},
		},
		"temperature": 0,
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": r.SchemaName, "strict": true, "schema": r.Schema},
		},
	})
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions",
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("call model: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("model answered %d", res.StatusCode)
	}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, maxReply)).Decode(&reply); err != nil {
		return fmt.Errorf("decode reply: %w", err)
	}
	if len(reply.Choices) == 0 {
		return errors.New("model gave no choice")
	}
	if err = json.Unmarshal([]byte(reply.Choices[0].Message.Content), out); err != nil {
		return fmt.Errorf("decode answer: %w", err)
	}
	return nil
}
