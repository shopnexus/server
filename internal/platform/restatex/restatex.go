// Package restatex serves this process's Restate services, registers them
// with the Restate server, and holds what every service uses inside a
// handler.
//
// Every service follows one rule. A restate.Run step only reads or writes a
// store, so any error it returns is a fault Restate retries. Every business
// decision is taken between steps, on values a step returned and the journal
// kept, and code outside a step uses only those values. A replay therefore
// takes the same decisions, and a retried write lands the same row: Save
// accepts a row that already carries its write ID.
package restatex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/server"
)

// Endpoint is the HTTP/1.1 server Restate calls to run handlers.
type Endpoint struct {
	srv *http.Server
	ln  net.Listener
}

// Serve binds defs and serves them on addr; "127.0.0.1:0" picks a free port.
// With an identityKey from IdentityKey, it runs only requests the Restate
// server signed.
func Serve(ctx context.Context, addr, identityKey string, defs ...restate.ServiceDefinition) (*Endpoint, error) {
	r := server.NewRestate().Bidirectional(false)
	if identityKey != "" {
		r = r.WithIdentityV1(identityKey)
	}
	for _, d := range defs {
		r = r.Bind(d)
	}
	h, err := r.Handler()
	if err != nil {
		return nil, fmt.Errorf("build restate handler: %w", err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	e := &Endpoint{srv: &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}, ln: ln}
	// Serve returns http.ErrServerClosed after Close. A listener failure
	// stops the endpoint, and Restate's calls to it fail.
	go func() { _ = e.srv.Serve(ln) }()
	return e, nil
}

// URL is the address Restate calls when RESTATE_ENDPOINT_URL is empty.
func (e *Endpoint) URL() string { return "http://" + e.ln.Addr().String() }

func (e *Endpoint) Close(ctx context.Context) error {
	if err := e.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down restate endpoint: %w", err)
	}
	return nil
}

// Register asks the Restate server at adminURL to discover the services
// served at endpointURL, replacing an earlier registration of the same names.
// HTTP/1.1 is used because the endpoint is a standard library server.
func Register(ctx context.Context, adminURL, endpointURL string) error {
	body, err := json.Marshal(map[string]any{"uri": endpointURL, "force": true, "use_http_11": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(adminURL, "/")+"/deployments", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("register restate endpoint: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("register restate endpoint: %s: %s", res.Status, msg)
	}
	return nil
}

// Healthy reports whether the Restate admin API answers.
func Healthy(ctx context.Context, adminURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(adminURL, "/")+"/health", http.NoBody)
	if err != nil {
		return fmt.Errorf("build health request: %w", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("restate admin health: %w", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("restate admin health: %s", res.Status)
	}
	return nil
}
