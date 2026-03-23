package restateclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is a simple HTTP client for calling Restate services via the ingress endpoint.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{},
	}
}

// Call invokes a Restate service method and decodes the response.
func Call[O any](ctx context.Context, c *Client, service, method string, input any) (O, error) {
	var zero O

	body, err := json.Marshal(input)
	if err != nil {
		return zero, fmt.Errorf("restate: marshal input: %w", err)
	}

	url := fmt.Sprintf("%s/%s/%s", c.BaseURL, service, method)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("restate: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("restate: call %s/%s: %w", service, method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("restate: %s/%s returned %d: %s", service, method, resp.StatusCode, string(respBody))
	}

	if err := json.NewDecoder(resp.Body).Decode(&zero); err != nil {
		return zero, fmt.Errorf("restate: decode response from %s/%s: %w", service, method, err)
	}

	return zero, nil
}

// Send invokes a Restate service method without decoding the response body.
func Send(ctx context.Context, c *Client, service, method string, input any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("restate: marshal input: %w", err)
	}

	url := fmt.Sprintf("%s/%s/%s", c.BaseURL, service, method)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("restate: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("restate: call %s/%s: %w", service, method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("restate: %s/%s returned %d: %s", service, method, resp.StatusCode, string(respBody))
	}

	return nil
}
