package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type frankfurterResponse struct {
	Base  string             `json:"base"`
	Date  string             `json:"date"`
	Rates map[string]float64 `json:"rates"`
}

type frankfurterClient struct {
	baseURL string
	http    *http.Client
}

// NewFrankfurter returns a Client backed by the Frankfurter API.
// baseURL is typically "https://api.frankfurter.dev" (no trailing slash).
// httpClient controls timeout and transport; callers should pass one
// with an explicit timeout (e.g. 5s).
func NewFrankfurter(baseURL string, httpClient *http.Client) Client {
	return &frankfurterClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    httpClient,
	}
}

func (c *frankfurterClient) FetchLatest(
	ctx context.Context, base string, targets []string,
) (Snapshot, error) {
	q := url.Values{}
	q.Set("base", base)
	if len(targets) > 0 {
		q.Set("symbols", strings.Join(targets, ","))
	}
	u := fmt.Sprintf("%s/v1/latest?%s", c.baseURL, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("exchange: build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("exchange: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Snapshot{}, fmt.Errorf("exchange: upstream status %d", resp.StatusCode)
	}

	var body frankfurterResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Snapshot{}, fmt.Errorf("exchange: decode: %w", err)
	}

	return Snapshot{
		Base:      body.Base,
		Rates:     body.Rates,
		FetchedAt: time.Now().UTC(),
	}, nil
}
