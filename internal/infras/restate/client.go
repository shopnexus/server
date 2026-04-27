package restateclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"
	restate "github.com/restatedev/sdk-go"
)

// Strips leading "[CODE] " markers that Restate prepends to error messages.
// Matches one or more consecutive brackets so doubled prefixes (which can
// appear after multi-hop service calls) collapse in a single pass.
var codePrefix = regexp.MustCompile(`^(?:\[\d+\]\s*)+`)

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

// newRestateTransport returns an http.Transport tuned for a single
// hot-path destination (the Restate ingress). Default net/http caps
// MaxIdleConnsPerHost at 2, which serializes every concurrent request
// past the second onto new TCP handshakes — turning localhost ingress
// calls into 40–90ms round-trips. Raise both pool limits to match the
// number of concurrent in-flight requests we expect from Echo.
func newRestateTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: newRestateTransport(),
		},
	}
}

// CallOption customises a Call / Send invocation.
type CallOption func(*callOpts)

type callOpts struct {
	// IdempotencyKey is sent as the `idempotency-key` header. Restate dedupes
	// requests with the same key, so a retried call returns the cached result
	// instead of re-executing the handler. Auto-generated per logical Call if unset.
	IdempotencyKey string
	// MaxRetries caps retry attempts for transient failures (network errors, 5xx).
	// Terminal errors (4xx from Restate) and context cancellation short-circuit.
	MaxRetries int
	// BaseBackoff is the initial sleep before the first retry; doubles each attempt.
	BaseBackoff time.Duration
}

func defaultCallOpts() callOpts {
	return callOpts{
		MaxRetries:  3,
		BaseBackoff: 100 * time.Millisecond,
	}
}

// WithIdempotencyKey pins the idempotency-key header to a caller-supplied value,
// so manual external retries (e.g. frontend re-submit) dedupe against the same key.
func WithIdempotencyKey(key string) CallOption {
	return func(o *callOpts) { o.IdempotencyKey = key }
}

// WithMaxRetries overrides the default retry cap. Use 0 to disable retries.
func WithMaxRetries(n int) CallOption {
	return func(o *callOpts) { o.MaxRetries = n }
}

// WithBaseBackoff overrides the initial backoff delay.
func WithBaseBackoff(d time.Duration) CallOption {
	return func(o *callOpts) { o.BaseBackoff = d }
}

// Call invokes a Restate service method.
//
// Path selection:
//   - If ctx is a restate.Context (caller is inside a Restate handler), the call
//     is journaled into the parent handler's invocation: the result is recorded
//     and replayed on parent retry/restart. Exactly-once semantics, no caller-side
//     retry needed (Restate engine handles delivery).
//   - Otherwise (plain context.Context, e.g. from transport/echo), the call goes
//     out as HTTP POST to Restate ingress with caller-side retry + idempotency-key
//     dedupe. Transient failures (network, 5xx) retried with exponential backoff;
//     terminal 4xx short-circuit.
func Call[O any](ctx context.Context, c *Client, service, method string, input any, opts ...CallOption) (O, error) {
	if rctx, ok := ctx.(restate.Context); ok {
		return restate.Service[O](rctx, service, method).Request(input)
	}

	o := defaultCallOpts()
	for _, opt := range opts {
		opt(&o)
	}
	if o.IdempotencyKey == "" {
		o.IdempotencyKey = uuid.NewString()
	}

	var zero O
	body, err := json.Marshal(input)
	if err != nil {
		return zero, fmt.Errorf("restate: marshal input: %w", err)
	}
	url := fmt.Sprintf("%s/%s/%s", c.BaseURL, service, method)

	var lastErr error
	for attempt := 0; attempt <= o.MaxRetries; attempt++ {
		out, err, retryable := doCall[O](ctx, c, url, service, method, body, o.IdempotencyKey)
		if err == nil {
			return out, nil
		}
		if !retryable {
			return out, err
		}
		lastErr = err
		if attempt < o.MaxRetries {
			if waitErr := sleepBackoff(ctx, attempt, o.BaseBackoff); waitErr != nil {
				return zero, waitErr
			}
		}
	}
	return zero, lastErr
}

// doCall performs a single HTTP attempt. The bool return indicates whether the
// error class is transient (retryable).
func doCall[O any](
	ctx context.Context,
	c *Client,
	url, service, method string,
	body []byte,
	idemKey string,
) (O, error, bool) {
	var zero O
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("restate: create request: %w", err), false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("idempotency-key", idemKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		// Context cancellation is not retryable; bubble up the caller's choice.
		if ctx.Err() != nil {
			return zero, ctx.Err(), false
		}
		// Network/transport error — transient.
		return zero, fmt.Errorf("restate: call %s/%s: %w", service, method, err), true
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var out O
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return zero, fmt.Errorf("restate: decode response from %s/%s: %w", service, method, err), false
		}
		return out, nil, false
	}

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		// Terminal: caller-side error, don't retry.
		return zero, restate.TerminalError(
			parseRestateError(resp.StatusCode, respBody, service, method),
			restate.Code(uint16(resp.StatusCode)),
		), false
	}
	// 5xx / upstream-side failure — transient.
	return zero, fmt.Errorf("restate: %s/%s returned %d: %s", service, method, resp.StatusCode, string(respBody)), true
}

// Send invokes a Restate service method as fire-and-forget.
//
// Path selection mirrors Call:
//   - restate.Context caller → journaled ServiceSend (durable, exactly-once delivery
//     orchestrated by Restate against the parent handler's journal).
//   - plain context.Context → HTTP POST with retry + idempotency-key.
func Send(ctx context.Context, c *Client, service, method string, input any, opts ...CallOption) error {
	if rctx, ok := ctx.(restate.Context); ok {
		restate.ServiceSend(rctx, service, method).Send(input)
		return nil
	}

	o := defaultCallOpts()
	for _, opt := range opts {
		opt(&o)
	}
	if o.IdempotencyKey == "" {
		o.IdempotencyKey = uuid.NewString()
	}

	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("restate: marshal input: %w", err)
	}
	url := fmt.Sprintf("%s/%s/%s", c.BaseURL, service, method)

	var lastErr error
	for attempt := 0; attempt <= o.MaxRetries; attempt++ {
		err, retryable := doSend(ctx, c, url, service, method, body, o.IdempotencyKey)
		if err == nil {
			return nil
		}
		if !retryable {
			return err
		}
		lastErr = err
		if attempt < o.MaxRetries {
			if waitErr := sleepBackoff(ctx, attempt, o.BaseBackoff); waitErr != nil {
				return waitErr
			}
		}
	}
	return lastErr
}

func doSend(
	ctx context.Context,
	c *Client,
	url, service, method string,
	body []byte,
	idemKey string,
) (error, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("restate: create request: %w", err), false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("idempotency-key", idemKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err(), false
		}
		return fmt.Errorf("restate: call %s/%s: %w", service, method, err), true
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil, false
	}

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return restate.TerminalError(
			parseRestateError(resp.StatusCode, respBody, service, method),
			restate.Code(uint16(resp.StatusCode)),
		), false
	}
	return fmt.Errorf("restate: %s/%s returned %d: %s", service, method, resp.StatusCode, string(respBody)), true
}

// sleepBackoff sleeps for BaseBackoff * 2^attempt, aborting if ctx is cancelled.
func sleepBackoff(ctx context.Context, attempt int, base time.Duration) error {
	backoff := base * time.Duration(1<<attempt)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(backoff):
		return nil
	}
}
