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
// to look at, and the JSON schema the answer must fit.
type Request struct {
	System, User string
	// Images are JPEGs sent after User, in order, as data URLs.
	Images     [][]byte
	SchemaName string
	Schema     json.RawMessage
}

// ErrRefused is a 4xx answer other than 429: the same request would be
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
	HTTP                   *http.Client
}

// maxReply caps how much of a reply is read.
const maxReply = 1 << 20

func (c OpenAI) Complete(ctx context.Context, r Request, out any) error {
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
	body, err := json.Marshal(map[string]any{
		"model": c.Model,
		"messages": []map[string]any{
			{"role": "system", "content": r.System},
			{"role": "user", "content": user},
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
		if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
			return fmt.Errorf("model answered %d: %w", res.StatusCode, ErrRefused)
		}
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
