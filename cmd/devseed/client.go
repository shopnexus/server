package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// client calls the API as a person, the way the app does.
type client struct {
	base string
	http *http.Client
}

// call sends body as JSON and decodes a 2xx response into out; either may be
// nil. headers are name, value pairs. Any other status is an error carrying
// the API's error code.
func (c client) call(ctx context.Context, token, method, path string, body, out any, headers ...string) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode the body of %s %s: %w", method, path, err)
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read the response of %s %s: %w", method, path, err)
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Code != "" {
			return fmt.Errorf("%s %s: %d %s: %s", method, path, res.StatusCode, e.Error.Code, e.Error.Message)
		}
		return fmt.Errorf("%s %s: %d", method, path, res.StatusCode)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode the response of %s %s: %w", method, path, err)
	}
	return nil
}
