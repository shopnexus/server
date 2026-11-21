// Package indexnow tells search engines which pages changed, through the
// IndexNow protocol (https://www.indexnow.org/documentation).
package indexnow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Endpoint is IndexNow's shared endpoint; it passes the URLs on to every
// engine that takes part.
const Endpoint = "https://api.indexnow.org/indexnow"

// MaxURLs is how many URLs one submission may carry.
const MaxURLs = 10_000

// Submission is one batch of changed URLs of host. KeyLocation is where
// the key file is served, https://<host>/<key>.txt.
type Submission struct {
	Host        string   `json:"host"`
	Key         string   `json:"key"`
	KeyLocation string   `json:"keyLocation"` //nolint:tagliatelle // IndexNow's own name
	URLs        []string `json:"urlList"`     //nolint:tagliatelle // IndexNow's own name
}

// StatusError is a submission IndexNow refused, with its HTTP status.
type StatusError struct{ Status int }

func (e *StatusError) Error() string { return fmt.Sprintf("indexnow answered %d", e.Status) }

// Retry reports whether IndexNow throttled or failed (429 or 5xx), which
// another try may get past.
func (e *StatusError) Retry() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= http.StatusInternalServerError
}

// Client submits to an IndexNow endpoint.
type Client struct {
	http     *http.Client
	endpoint string
}

// New submits through client to endpoint: Endpoint, or a test server.
func New(client *http.Client, endpoint string) *Client {
	return &Client{http: client, endpoint: endpoint}
}

// Submit posts s. A reply other than 200 or 202 is a *StatusError; a
// request that did not reach IndexNow is another error.
func (c *Client) Submit(ctx context.Context, s Submission) error {
	if len(s.URLs) == 0 || len(s.URLs) > MaxURLs {
		return fmt.Errorf("indexnow takes 1 to %d URLs, not %d", MaxURLs, len(s.URLs))
	}
	body, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode the submission: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build the submission: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("submit to indexnow: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		return &StatusError{Status: res.StatusCode}
	}
	return nil
}
