package restateclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	restate "github.com/restatedev/sdk-go"
)

var codePrefix = regexp.MustCompile(`^\[\d+\]\s*`)

// parseRestateError tries to extract the original error message from a Restate JSON error response.
// Falls back to a generic message if the body isn't parseable.
func parseRestateError(statusCode int, body []byte, service, method string) error {
	var parsed struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		// Strip Restate's "[CODE] " prefix (e.g. "[409] Sorry, ...") to avoid duplication when re-wrapped
		msg := codePrefix.ReplaceAllString(parsed.Message, "")
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("restate: %s/%s returned %d: %s", service, method, statusCode, string(body))
}

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
		// 4xx from Restate ingress means the callee returned a terminal error — propagate as terminal with original code
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return zero, restate.TerminalError(parseRestateError(resp.StatusCode, respBody, service, method), restate.Code(uint16(resp.StatusCode)))
		}
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
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return restate.TerminalError(parseRestateError(resp.StatusCode, respBody, service, method), restate.Code(uint16(resp.StatusCode)))
		}
		return fmt.Errorf("restate: %s/%s returned %d: %s", service, method, resp.StatusCode, string(respBody))
	}

	return nil
}
