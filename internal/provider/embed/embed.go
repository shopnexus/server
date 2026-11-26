// Package embed holds Model, what the modules embed text and photos with,
// and OpenAI, the Model of text over an OpenAI-compatible POST /embeddings,
// which the gateway serves. Photos go to module ai (ai/service.Photos) as
// data URL strings, which ImageInput writes. It knows nothing about what
// the vectors are for.
package embed

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"strings"
)

// Model embeds inputs with one model; the vectors come back in input order.
type Model interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// ErrRefused means the endpoint refused the input itself; sending it again
// will not help. Load, budget and rate refusals (429, 5xx) are other errors.
var ErrRefused = errors.New("embed: the input was refused")

// ErrBudgetSpent is a 402: the caller's budget or the provider's credit is
// spent, so the same input fails until it is topped up or resets.
var ErrBudgetSpent = errors.New("embed: the budget is spent")

// OpenAI is a Model over an OpenAI-compatible API.
type OpenAI struct {
	BaseURL string // ends in /v1
	APIKey  string // empty sends no Authorization header
	Model   string
	Dims    int // the length every vector must have
	// Fallbacks are the models a gateway tries after Model, in order;
	// Extra joins the body as it is and Header the request's headers.
	Fallbacks []string
	Extra     map[string]any
	Header    http.Header
	HTTP      *http.Client
}

// APIError is an answer other than 200. A 400, 404, 413 or 422 is
// ErrRefused.
type APIError struct {
	Model  string
	Status int
	// Type is the gateway's error type: Bifrost's X-Bifrost-Error-Type, else
	// the body's top-level type.
	Type string
}

// GatewayErrorType is the gateway's kind of the error, empty when none.
func (e *APIError) GatewayErrorType() string { return e.Type }

// errorType reads the gateway's error type of an answer.
func errorType(res *http.Response) string {
	if h := res.Header.Get("X-Bifrost-Error-Type"); h != "" {
		return h
	}
	var body struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 4<<10)).Decode(&body)
	return body.Type
}

func (e *APIError) Error() string { return fmt.Sprintf("%s answered %d", e.Model, e.Status) }

// HTTPStatus is the answer's status.
func (e *APIError) HTTPStatus() int { return e.Status }

func (e *APIError) Is(target error) bool {
	if target == ErrBudgetSpent {
		return e.Status == http.StatusPaymentRequired
	}
	switch e.Status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return target == ErrRefused
	default:
		return false
	}
}

// maxHalf is the largest magnitude halfvec stores.
const maxHalf = 65504

// maxReply caps how much of a reply is read: 16 inputs of 1152 floats as
// JSON are about 400 KB.
const maxReply = 8 << 20

func (c OpenAI) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	req := map[string]any{}
	maps.Copy(req, c.Extra)
	maps.Copy(req, map[string]any{"model": c.Model, "input": inputs, "encoding_format": "float"})
	if len(c.Fallbacks) > 0 {
		req["fallbacks"] = c.Fallbacks
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	maps.Copy(hr.Header, c.Header)
	if c.APIKey != "" {
		hr.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	hr.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", c.Model, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, &APIError{Model: c.Model, Status: res.StatusCode, Type: errorType(res)}
	}
	var reply struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, maxReply)).Decode(&reply); err != nil {
		return nil, fmt.Errorf("decode %s's reply: %w", c.Model, err)
	}
	if len(reply.Data) != len(inputs) {
		return nil, fmt.Errorf("%s gave %d vectors for %d inputs", c.Model, len(reply.Data), len(inputs))
	}
	out := make([][]float32, len(inputs))
	for _, d := range reply.Data {
		if d.Index < 0 || d.Index >= len(out) || out[d.Index] != nil {
			return nil, fmt.Errorf("%s gave index %d twice or out of range", c.Model, d.Index)
		}
		if len(d.Embedding) != c.Dims {
			return nil, fmt.Errorf("%s gave %d dimensions, want %d", c.Model, len(d.Embedding), c.Dims)
		}
		v := make([]float32, c.Dims)
		for i, x := range d.Embedding {
			// halfvec holds ±65504; a value past it, or past float32, would
			// make Postgres refuse the vector, so it is refused here.
			if math.IsNaN(x) || math.Abs(x) > maxHalf {
				return nil, fmt.Errorf("%s gave a value outside ±%v", c.Model, maxHalf)
			}
			v[i] = float32(x)
		}
		out[d.Index] = v
	}
	return out, nil
}

// ImageInput is a JPEG as an input string.
func ImageInput(jpeg []byte) string {
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpeg)
}
