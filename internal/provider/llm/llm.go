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
	"maps"
	"net/http"
	"regexp"
	"strings"
)

// Request is one question: the instructions, the text to read, the photos
// to look at, and the JSON schema the answer must fit, if any.
type Request struct {
	System, User string
	// Images are JPEGs sent after User, in order, as data URLs.
	Images     [][]byte
	SchemaName string
	Schema     json.RawMessage
}

// ErrRefused is a 4xx answer other than 402 and 429: the same request would be
// refused again, so a caller should not retry it. The error is an *APIError.
var ErrRefused = errors.New("model refused the request")

// ErrBudgetSpent is a 402: the caller's budget or the provider's credit is
// spent, so the same request fails until it is topped up or resets.
var ErrBudgetSpent = errors.New("model budget spent")

// Client asks a model.
type Client interface {
	// Complete asks for an answer that fits r.Schema and decodes it into out.
	Complete(ctx context.Context, r Request, out any) error
}

// OpenAI is a Client for an OpenAI-compatible API, such as a gateway in
// front of several models.
type OpenAI struct {
	BaseURL, APIKey, Model string
	// Effort is sent as reasoning_effort; empty leaves the model's default.
	Effort string
	// Fallbacks are the models a gateway tries after Model, in order;
	// Extra joins the body as it is and Header the request's headers.
	Fallbacks []string
	Extra     map[string]any
	Header    http.Header
	HTTP      *http.Client
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
	req := map[string]any{}
	maps.Copy(req, c.Extra)
	maps.Copy(req, map[string]any{
		"model": c.Model,
		"messages": []map[string]any{
			{"role": "system", "content": r.System},
			{"role": "user", "content": user},
		},
		"temperature": 0,
	})
	if len(c.Fallbacks) > 0 {
		req["fallbacks"] = c.Fallbacks
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
	maps.Copy(hr.Header, c.Header)
	hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	hr.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("call model: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		defer func() { _ = res.Body.Close() }()
		msg, kind := errorMessage(res.Body)
		if h := res.Header.Get(GatewayErrorHeader); h != "" {
			kind = h
		}
		return nil, &APIError{Status: res.StatusCode, Message: msg, Type: kind}
	}
	return res, nil
}

// APIError is an answer other than 200, with the message its body gave.
// A 4xx other than 402 and 429 is ErrRefused: 402 is a spent budget or
// credit and 429 a rate, so neither is the request's fault.
type APIError struct {
	Status  int
	Message string
	// Type is the gateway's error type: Bifrost's X-Bifrost-Error-Type, else
	// the body's top-level type.
	Type string
}

// GatewayErrorHeader is where Bifrost names the kind of an error.
const GatewayErrorHeader = "X-Bifrost-Error-Type"

// GatewayErrorType is the gateway's kind of the error, empty when none.
func (e *APIError) GatewayErrorType() string { return e.Type }

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("model answered %d", e.Status)
	}
	return fmt.Sprintf("model answered %d: %s", e.Status, e.Message)
}

// HTTPStatus is the answer's status.
func (e *APIError) HTTPStatus() int { return e.Status }

func (e *APIError) Is(target error) bool {
	switch target {
	case ErrBudgetSpent:
		return e.Status == http.StatusPaymentRequired
	case ErrRefused:
		return e.Status >= 400 && e.Status < 500 &&
			e.Status != http.StatusTooManyRequests && e.Status != http.StatusPaymentRequired
	default:
		return false
	}
}

// maxErrorBody caps how much of an error answer is read.
const maxErrorBody = 4 << 10

// errorMessage is the message of an OpenAI-style error body, else its
// first line, at most 300 bytes and without anything that looks like a key,
// and the body's top-level type.
func errorMessage(r io.Reader) (msg, kind string) {
	b, _ := io.ReadAll(io.LimitReader(r, maxErrorBody))
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg = string(b)
	if json.Unmarshal(b, &body) == nil {
		kind = body.Type
		if body.Error.Message != "" {
			msg = body.Error.Message
		}
	}
	msg, _, _ = strings.Cut(strings.TrimSpace(msg), "\n")
	msg = secretLike.ReplaceAllString(msg, "***")
	if len(msg) > 300 {
		msg = strings.ToValidUTF8(msg[:300], "")
	}
	return msg, kind
}

// secretLike is an API key or bearer token in an error message.
var secretLike = regexp.MustCompile(`(?i)(sk-[\w-]{6,}|bearer\s+\S+)`)
