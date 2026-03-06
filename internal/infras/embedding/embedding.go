package embedding

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
)

// Config holds configuration for the embedding service HTTP client.
type Config struct {
	URL string `yaml:"url"` // e.g. "http://localhost:8000"
}

// Client is an HTTP client for the Python embedding service.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new embedding service client.
func NewClient(cfg Config) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.URL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// EmbedResult holds the dense and sparse vectors produced by the embedding service.
type EmbedResult struct {
	Dense  []float32          `json:"dense"`
	Sparse map[uint32]float32 `json:"sparse"`
}

// embedRequest is the JSON request body sent to the /embed endpoint.
type embedRequest struct {
	Texts []string `json:"texts"`
}

// embedResponseEntry is a single embedding entry from the service response.
// The sparse field uses string keys because JSON object keys are always strings.
type embedResponseEntry struct {
	Dense  []float32         `json:"dense"`
	Sparse map[string]float32 `json:"sparse"`
}

// embedResponse is the JSON response body from the /embed endpoint.
type embedResponse struct {
	Embeddings []embedResponseEntry `json:"embeddings"`
}

// Embed sends texts to the embedding service and returns dense+sparse vectors.
func (c *Client) Embed(ctx context.Context, texts []string) ([]EmbedResult, error) {
	// Marshal request body.
	body, err := sonic.Marshal(embedRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("embedding: marshal request: %w", err)
	}

	// Build HTTP request with context.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute request.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: service returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Unmarshal response.
	var result embedResponse
	if err := sonic.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("embedding: unmarshal response: %w", err)
	}

	// Convert string-keyed sparse maps to uint32-keyed maps.
	out := make([]EmbedResult, len(result.Embeddings))
	for i, entry := range result.Embeddings {
		sparse := make(map[uint32]float32, len(entry.Sparse))
		for k, v := range entry.Sparse {
			idx, err := strconv.ParseUint(k, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("embedding: invalid sparse index %q in result %d: %w", k, i, err)
			}
			sparse[uint32(idx)] = v
		}
		out[i] = EmbedResult{
			Dense:  entry.Dense,
			Sparse: sparse,
		}
	}

	return out, nil
}
