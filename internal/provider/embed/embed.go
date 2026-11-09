// Package embed turns text and photos into vectors through an
// OpenAI-compatible POST /embeddings: LiteLLM's aliases in the cluster, TEI
// and ai locally. An image goes as a data URL string, the convention LiteLLM
// uses for multimodal embeddings. It knows nothing about what the vectors are
// for.
package embed

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// OpenAI is a Model over an OpenAI-compatible API.
type OpenAI struct {
	BaseURL string // ends in /v1
	APIKey  string // empty sends no Authorization header
	Model   string
	Dims    int // the length every vector must have
	HTTP    *http.Client
}

// maxReply caps how much of a reply is read: 16 inputs of 1152 floats as
// JSON are about 400 KB.
const maxReply = 8 << 20

func (c OpenAI) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": c.Model, "input": inputs, "encoding_format": "float"})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", c.Model, err)
	}
	defer func() { _ = res.Body.Close() }()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return nil, fmt.Errorf("%w: %s answered %d", ErrRefused, c.Model, res.StatusCode)
	default:
		return nil, fmt.Errorf("%s answered %d", c.Model, res.StatusCode)
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
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, fmt.Errorf("%s gave a value that is not finite", c.Model)
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
