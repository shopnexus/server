package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Resend sends through Resend's HTTP API from one address of a domain
// verified there.
type Resend struct {
	// URL is the emails endpoint; tests point it at a server of their own.
	URL    string
	apiKey string
	from   string
	http   *http.Client
}

func NewResend(apiKey, from string) *Resend {
	return &Resend{URL: "https://api.resend.com/emails", apiKey: apiKey, from: from, http: &http.Client{Timeout: 10 * time.Second}}
}

func (s *Resend) Send(ctx context.Context, to, subject, body string) error {
	payload, err := json.Marshal(map[string]any{"from": s.from, "to": []string{to}, "subject": subject, "text": body})
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("post to resend: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("resend answered %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}
