package handler_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/handler"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

var member = authn.ConsoleMember{MemberID: uuid.NewV7(), Role: "moderator"}

// serverOn serves the agent endpoints over a service whose agent is h, as
// a signed-in moderator; the guards only put the member in the context.
func serverOn(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	agent := httptest.NewServer(h)
	t.Cleanup(agent.Close)
	svc := service.New(service.Deps{BaseURL: agent.URL, Token: "service-token", HTTP: &http.Client{}, Now: time.Now})
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

// The stream reaches the console event by event as the agent writes it,
// with the stream's headers and the member's Last-Event-ID, past the
// server's write timeout; closing the console's side ends the agent's.
// UC-MOD-08.
func TestEventsProxy(t *testing.T) {
	t.Parallel()
	turn := uuid.NewV7()
	release := make(chan struct{})
	gone := make(chan struct{})
	srv := serverOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/turns/"+turn.String()+"/events" || r.Header.Get("Last-Event-ID") != "41" ||
			r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("X-Console-Member") != member.MemberID.String() {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Error("the stream cannot flush")
			return
		}
		_, _ = fmt.Fprint(w, "id: 42\nevent: step.started\ndata: {\"seq\":42,\"type\":\"step.started\",\"title\":\"Read\"}\n\n")
		fl.Flush()
		<-release
		// Past the server's 500 ms write timeout.
		time.Sleep(700 * time.Millisecond)
		_, _ = fmt.Fprint(w, "id: 43\ndata: {\"seq\":43,\"type\":\"text.delta\"}\n\n")
		fl.Flush()
		<-r.Context().Done()
		close(gone)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/console/agent/turns/"+turn.String()+"/events", http.NoBody)
	req.Header.Set("Last-Event-ID", "41")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" ||
		res.Header.Get("Cache-Control") != "no-cache" || res.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	r := bufio.NewReader(res.Body)
	first := readEvent(t, r)
	if !strings.Contains(first, "id: 42") || !strings.Contains(first, `"title":"Read"`) {
		t.Fatalf("first event %q", first)
	}
	close(release)
	if second := readEvent(t, r); !strings.Contains(second, "id: 43") {
		t.Fatalf("second event %q", second)
	}
	cancel()
	select {
	case <-gone:
	case <-time.After(3 * time.Second):
		t.Fatal("the agent's stream stayed open after the console closed its side")
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

// A stream the agent refuses is an error answer before any event: an
// unknown turn is agent_turn_not_found.
func TestEventsOfAnUnknownTurn(t *testing.T) {
	t.Parallel()
	srv := serverOn(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found"}`))
	}))
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

// Pause, end, steer and the answers to a clarifying question go to the
// turn's paths and answer with the turn; answers that do not fit are
// agent_answers_invalid and a turn in the wrong state agent_turn_state.
func TestTurnControls(t *testing.T) {
	t.Parallel()
	turn := uuid.NewV7()
	srv := serverOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/turns/" + turn.String() + "/pause":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{}`))
		case "/turns/" + turn.String() + "/end":
			_, _ = w.Write([]byte(`{"status":"CANCELLED"}`))
		case "/turns/" + turn.String() + "/steer":
			if !strings.Contains(string(body), `"text":"Only Hanoi"`) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = fmt.Fprintf(w, `{"steer_id":%q,"resumed":true}`, uuid.NewV7())
		case "/turns/" + turn.String() + "/input":
			if !strings.Contains(string(body), `"answers":{"range":"week"}`) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"bad_request"}`))
				return
			}
			_, _ = fmt.Fprintf(w, `{"turn_id":%q}`, turn)
		default:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"turn_state"}`))
		}
	}))
	base := srv.URL + "/v1/console/agent/turns/" + turn.String()
	for _, c := range []struct {
		path, body, want string
		status           int
	}{
		{"/pause", "", `"turn_id":"` + turn.String(), http.StatusAccepted},
		{"/end", "", `"status":"cancelled"`, http.StatusOK},
		{"/steer", `{"text":"Only Hanoi"}`, `"resumed":true`, http.StatusAccepted},
		{"/input", `{"answers":{"range":"week"}}`, `"turn_id"`, http.StatusAccepted},
		{"/input", `{"answers":{"range":"year"}}`, `"code":"agent_answers_invalid"`, http.StatusUnprocessableEntity},
		{"/continue", "", `"code":"agent_turn_state"`, http.StatusConflict},
	} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, base+c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != c.status || !strings.Contains(string(got), c.want) {
			t.Errorf("%s: %d %s", c.path, res.StatusCode, got)
		}
	}
}
