package handler_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/handler"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

var member = authn.ConsoleMember{MemberID: uuid.NewV7(), Role: "moderator"}

// events is a service whose Events is the pipe it holds for turn, after
// the Last-Event-ID it got.
type events struct {
	service.Service

	turn  uuid.UUID
	body  io.ReadCloser
	after chan string
}

// Events closes the stream when ctx ends, as the service's does.
func (e events) Events(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, last string) (io.ReadCloser, error) {
	if turn != e.turn || caller != member {
		return nil, domain.ErrTurnNotFound
	}
	e.after <- last
	go func() {
		<-ctx.Done()
		_ = e.body.Close()
	}()
	return e.body, nil
}

// serverOn serves the agent endpoints over svc as a signed-in moderator,
// with a write timeout of 500 ms.
func serverOn(t *testing.T, svc service.Service) *httptest.Server {
	t.Helper()
	router := httpx.NewRouter()
	signedIn := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(authn.WithConsoleMember(r.Context(), member)))
		})
	}
	handler.New(svc, slog.New(slog.DiscardHandler)).Register(router, signedIn, signedIn)
	srv := httptest.NewUnstartedServer(router)
	srv.Config.WriteTimeout = 500 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// The stream reaches the console event by event as the service writes it,
// with the stream's headers and the member's Last-Event-ID, past the
// server's write timeout; closing the console's side closes the service's
// stream. UC-MOD-08.
func TestEventsStream(t *testing.T) {
	t.Parallel()
	turn := uuid.NewV7()
	r, w := io.Pipe()
	svc := events{turn: turn, body: r, after: make(chan string, 1)}
	srv := serverOn(t, svc)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/console/agent/turns/"+turn.String()+"/events", http.NoBody)
	req.Header.Set("Last-Event-ID", "41")
	go func() {
		_, _ = io.WriteString(w, "id: 42\nevent: step.started\ndata: {\"seq\":42,\"title\":\"Read\"}\n\n")
		// Past the server's 500 ms write timeout.
		time.Sleep(700 * time.Millisecond)
		_, _ = io.WriteString(w, "id: 43\ndata: {\"seq\":43}\n\n")
	}()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" ||
		res.Header.Get("Cache-Control") != "no-cache" || res.Header.Get("X-Accel-Buffering") != "no" || <-svc.after != "41" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	lines := bufio.NewReader(res.Body)
	if first := readEvent(t, lines); !strings.Contains(first, "id: 42") || !strings.Contains(first, `"title":"Read"`) {
		t.Fatalf("first event %q", first)
	}
	if second := readEvent(t, lines); !strings.Contains(second, "id: 43") {
		t.Fatalf("second event %q", second)
	}
	cancel()
	closed := make(chan error, 1)
	go func() {
		for {
			if _, writeErr := io.WriteString(w, "id: 44\n\n"); writeErr != nil {
				closed <- writeErr
				return
			}
		}
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		_ = w.Close()
		t.Fatal("the service's stream stayed open after the console closed its side")
	}
}

// readEvent reads up to the blank line that ends an event.
func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read an event: %v after %q", err, b.String())
		}
		if line == "\n" {
			return b.String()
		}
		b.WriteString(line)
	}
}

// A stream the service refuses is an error answer before any event.
func TestEventsOfAnUnknownTurn(t *testing.T) {
	t.Parallel()
	srv := serverOn(t, events{turn: uuid.NewV7(), after: make(chan string, 1)})
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v1/console/agent/turns/"+uuid.NewV7().String()+"/events",
		http.NoBody)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != http.StatusNotFound || body.Error.Code != "agent_turn_not_found" {
		t.Fatalf("%d %+v", res.StatusCode, body)
	}
}
