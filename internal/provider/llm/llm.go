// Package llm asks a language model for structured answers through an
// OpenAI-compatible chat completions endpoint. It knows nothing about what
// the answers are for; callers write the prompt and the schema.
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Request is one question: the instructions, the text to read, the photos
// to look at, and the JSON schema the answer must fit. Stream sends no
// schema: its answer is plain text.
type Request struct {
	System, User string
	// Images are JPEGs sent after User, in order, as data URLs.
	Images     [][]byte
	SchemaName string
	Schema     json.RawMessage
}

// ErrRefused is a 4xx answer other than 402 and 429: the same request would be
// refused again, so a caller should not retry it.
var ErrRefused = errors.New("model refused the request")

// Client asks a model.
type Client interface {
	// Complete asks for an answer that fits r.Schema and decodes it into out.
	Complete(ctx context.Context, r Request, out any) error
}

// OpenAI is a Client for an OpenAI-compatible API, such as a proxy in front
// of several models.
type OpenAI struct {
	BaseURL, APIKey, Model string
	// Effort is sent as reasoning_effort; empty leaves the model's default.
	Effort string
	HTTP   *http.Client
}

// maxReply caps how much of a reply is read.
const maxReply = 1 << 20

func (c OpenAI) Complete(ctx context.Context, r Request, out any) error {
	res, err := c.post(ctx, r, false)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
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

// post sends r, as a stream when stream is set, and returns an OK reply
// for the caller to read and close.
func (c OpenAI) post(ctx context.Context, r Request, stream bool) (*http.Response, error) {
	var user any = r.User
	if len(r.Images) > 0 {
		parts := make([]map[string]any, 0, len(r.Images)+1)
		parts = append(parts, map[string]any{"type": "text", "text": r.User})
		for _, img := range r.Images {
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]string{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(img)},
			})
		}
		user = parts
	}
	req := map[string]any{
		"model": c.Model,
		"messages": []map[string]any{
			{"role": "system", "content": r.System},
			{"role": "user", "content": user},
		},
		"temperature": 0,
	}
	if r.Schema != nil {
		req["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": r.SchemaName, "strict": true, "schema": r.Schema},
		}
	}
	if stream {
		req["stream"] = true
	}
	if c.Effort != "" {
		req["reasoning_effort"] = c.Effort
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	hr.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("call model: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		// 402 is a spent budget or credit (OpenRouter through ai-gateway) and
		// 429 a rate: both pass, so neither is the request's fault.
		if res.StatusCode >= 400 && res.StatusCode < 500 &&
			res.StatusCode != http.StatusTooManyRequests && res.StatusCode != http.StatusPaymentRequired {
			return nil, fmt.Errorf("model answered %d: %w", res.StatusCode, ErrRefused)
		}
		return nil, fmt.Errorf("model answered %d", res.StatusCode)
	}
	return res, nil
}
