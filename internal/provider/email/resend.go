package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func NewResend(client *http.Client, apiKey, from string) *Resend {
	return &Resend{URL: "https://api.resend.com/emails", apiKey: apiKey, from: from, http: client}
}

func (s *Resend) Send(ctx context.Context, m Message) error {
	body := map[string]any{"from": s.from, "to": []string{m.To}, "subject": m.Subject, "text": m.Text}
	if m.HTML != "" {
		body["html"] = m.HTML
	}
	if len(m.Inline) > 0 {
		files := make([]map[string]string, len(m.Inline))
		for i, f := range m.Inline {
			files[i] = map[string]string{"filename": f.Name, "content": base64.StdEncoding.EncodeToString(f.Content), "content_id": f.CID}
		}
		body["attachments"] = files
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if m.Key != "" {
		req.Header.Set("Idempotency-Key", m.Key)
	}
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("post to resend: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		err := fmt.Errorf("resend answered %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
		// A 4xx other than 429 refuses the message itself: sending it again
		// fails again.
		if res.StatusCode/100 == 4 && res.StatusCode != http.StatusTooManyRequests {
			return fmt.Errorf("%w: %w", ErrRefused, err)
		}
		return err
	}
	return nil
}
