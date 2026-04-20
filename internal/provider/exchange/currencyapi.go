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

// currencyAPIResponse matches the https://currencyapi.com/ /v3/latest
// envelope. Only the fields we need are decoded.
type currencyAPIResponse struct {
	Meta struct {
		LastUpdatedAt string `json:"last_updated_at"`
	} `json:"meta"`
	Data map[string]struct {
		Code  string  `json:"code"`
		Value float64 `json:"value"`
	} `json:"data"`
}

type currencyAPIClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewCurrencyAPI returns a Client backed by currencyapi.com. baseURL is
// typically "https://api.currencyapi.com/v3" (no trailing slash). apiKey
// is required (free tier gives 300 req/month). httpClient controls
// timeout and transport.
func NewCurrencyAPI(baseURL, apiKey string, httpClient *http.Client) Client {
	return &currencyAPIClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    httpClient,
	}
}

func (c *currencyAPIClient) FetchLatest(
	ctx context.Context, base string, targets []string,
) (Snapshot, error) {
	q := url.Values{}
	q.Set("apikey", c.apiKey)
	q.Set("base_currency", base)
	if len(targets) > 0 {
		q.Set("currencies", strings.Join(targets, ","))
	}
	u := fmt.Sprintf("%s/latest?%s", c.baseURL, q.Encode())

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

	var body currencyAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Snapshot{}, fmt.Errorf("exchange: decode: %w", err)
	}

	rates := make(map[string]float64, len(body.Data))
	for code, v := range body.Data {
		rates[code] = v.Value
	}

	fetchedAt, err := time.Parse(time.RFC3339, body.Meta.LastUpdatedAt)
	if err != nil {
		fetchedAt = time.Now().UTC()
	}

	return Snapshot{
		Base:      base,
		Rates:     rates,
		FetchedAt: fetchedAt,
	}, nil
}
