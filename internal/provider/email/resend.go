package email

import (
	"bytes"
	"context"
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

// resendBody is the emails endpoint's request; Content goes as base64.
type resendBody struct {
	From        string       `json:"from"`
	To          []string     `json:"to"`
	Subject     string       `json:"subject"`
	Text        string       `json:"text"`
	HTML        string       `json:"html,omitempty"`
	Attachments []resendFile `json:"attachments,omitempty"`
}

type resendFile struct {
	Filename  string `json:"filename"`
	Content   []byte `json:"content"`
	ContentID string `json:"content_id"`
}

func (s *Resend) Send(ctx context.Context, m Message) error {
	files := make([]resendFile, len(m.Inline))
	for i, f := range m.Inline {
		files[i] = resendFile{Filename: f.Name, Content: f.Content, ContentID: f.CID}
	}
	// HTML goes as written: escaping <, > and & for JSON would add a fifth
	// to the body for nothing.
	var payload bytes.Buffer
	enc := json.NewEncoder(&payload)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(resendBody{From: s.from, To: []string{m.To}, Subject: m.Subject, Text: m.Text, HTML: m.HTML, Attachments: files}); err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, &payload)
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
