package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"
)

const (
	// agentServiceToken and agentMCPToken are the e2e server's
	// AGENT_SERVICE_TOKEN and AGENT_MCP_TOKEN.
	agentServiceToken = "e2e-agent-service-token-0123456789abcdef"
	agentMCPToken     = "e2e-agent-mcp-token-0123456789abcdefghij"
)

// agentDown is a subject or conversation id for which the stub agent
// fails with a 500, as an agent that broke would.
var agentDown = uuid.MustParse("0193e000-0000-7000-8000-00000000dead")

// agentRefusals are the questions the stub refuses, with the status and
// the code the agent service answers them with.
var agentRefusals = map[string]struct {
	status int
	code   string
}{
	"budget": {http.StatusTooManyRequests, "agent_budget"},
	"rate":   {http.StatusTooManyRequests, "rate_limited"},
	"busy":   {http.StatusConflict, "turn_in_progress"},
	"closed": {http.StatusConflict, "conversation_closed"},
	"long":   {http.StatusBadRequest, "input_rejected"},
	"down":   {http.StatusInternalServerError, "internal"},
}

// stubAgent stands in for the agent service, its API as shopnexus/agent
// serves it: snake_case JSON, errors as {code}, conversations in memory,
// one per subject. A question in agentRefusals is refused; any other gets
// an answer with one source. It refuses a request without the service
// token or the member's headers, so a test sees the server send them.
type stubAgent struct {
	srv           *httptest.Server
	mu            sync.Mutex
	conversations map[string]map[string]any
	turns         map[string][]map[string]any
	// asked is the last member and role a question came with, and
	// configs the aliases of the agents the questions went to.
	asked   [2]string
	configs []string
}

func newAgent() *stubAgent {
	a := &stubAgent{conversations: map[string]map[string]any{}, turns: map[string][]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /conversations", a.list)
	mux.HandleFunc("POST /conversations", a.open)
	mux.HandleFunc("GET /conversations/{id}", a.get)
	mux.HandleFunc("POST /conversations/{id}/turns", a.ask)
	mux.HandleFunc("GET /turns/{id}/events", a.events)
	a.runs(mux)
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+agentServiceToken || r.Header.Get("X-Console-Member") == "" ||
			r.Header.Get("X-Console-Role") == "" {
			refuse(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return a
}

func (a *stubAgent) url() string { return a.srv.URL }

func (a *stubAgent) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("subject_id") == agentDown.String() {
		refuse(w, http.StatusInternalServerError, "internal")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	items := []map[string]any{}
	for _, c := range a.conversations {
		var match bool
		switch {
		case q.Get("subject_id") != "":
			match = c["subject_id"] == q.Get("subject_id")
		case q.Get("subject_kind") != "":
			match = c["subject_kind"] == q.Get("subject_kind")
		default:
			match = c["subject_kind"] == nil && c["owner"] == r.Header.Get("X-Console-Member")
		}
		if match {
			items = append(items, public(c))
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *stubAgent) open(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SubjectKind string `json:"subject_kind"`
		SubjectID   string `json:"subject_id"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		refuse(w, http.StatusBadRequest, "bad_request")
		return
	}
	if in.SubjectID == agentDown.String() {
		refuse(w, http.StatusInternalServerError, "internal")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.conversations {
		if in.SubjectID != "" && c["subject_id"] == in.SubjectID {
			writeJSON(w, http.StatusOK, public(c))
			return
		}
	}
	c := map[string]any{
		"id": uuid.NewV7().String(), "updated_at": time.Now().UTC().Format(time.RFC3339), "owner": r.Header.Get("X-Console-Member"),
	}
	if in.SubjectID != "" {
		c["subject_kind"], c["subject_id"] = in.SubjectKind, in.SubjectID
	}
	a.conversations[c["id"].(string)] = c
	writeJSON(w, http.StatusOK, public(c))
}

func (a *stubAgent) get(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == agentDown.String() {
		refuse(w, http.StatusInternalServerError, "internal")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.conversations[r.PathValue("id")]
	if !ok {
		refuse(w, http.StatusNotFound, "not_found")
		return
	}
	out := public(c)
	turns := a.turns[r.PathValue("id")]
	if turns == nil {
		turns = []map[string]any{}
	}
	out["turns"] = turns
	writeJSON(w, http.StatusOK, out)
}

func (a *stubAgent) ask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text        string `json:"text"`
		AgentConfig struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Alias string `json:"alias"`
		} `json:"agent_config"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		refuse(w, http.StatusBadRequest, "bad_request")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	if _, ok := a.conversations[id]; !ok {
		refuse(w, http.StatusNotFound, "not_found")
		return
	}
	a.asked = [2]string{r.Header.Get("X-Console-Member"), r.Header.Get("X-Console-Role")}
	if no, ok := agentRefusals[strings.TrimSpace(in.Text)]; ok {
		refuse(w, no.status, no.code)
		return
	}
	turn := uuid.NewV7().String()
	a.turns[id] = append(a.turns[id], turnView(turn, id, r.Header.Get("X-Console-Member"), in.Text,
		map[string]any{"id": in.AgentConfig.ID, "name": in.AgentConfig.Name}))
	a.configs = append(a.configs, in.AgentConfig.Alias)
	writeJSON(w, http.StatusAccepted, map[string]any{"conversation_id": id, "turn_id": turn})
}

// turnView is a finished turn as the agent shows it, with one source.
func turnView(turn, conversation, member, text string, agent map[string]any) map[string]any {
	return map[string]any{
		"id": turn, "conversation_id": conversation, "member_id": member, "text": text, "answer": "Đã đọc.",
		"status": "SUCCEEDED", "blocks": []map[string]any{{"type": "text", "markdown": "Đã đọc [1]."}},
		"sources": []map[string]any{{
			"n": 1, "kind": "ticket", "record_id": conversation, "label": "Ticket", "call_id": uuid.NewV7(), "tool": "ticket_get",
		}},
		"outcome": nil, "continuable": false, "agent": agent, "steers": []any{}, "last_seq": 2,
		"usage":      map[string]any{"input_tokens": 10, "output_tokens": 5, "cost_usd": 0.001},
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
}

// events streams a finished turn's two events after Last-Event-ID.
func (*stubAgent) events(w http.ResponseWriter, r *http.Request) {
	switch uuid.MustParse(r.PathValue("id")) {
	case agentDown:
		refuse(w, http.StatusInternalServerError, "internal")
		return
	case agentMissing:
		refuse(w, http.StatusNotFound, "not_found")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	at := time.Now().UTC().Format(time.RFC3339)
	for i, typ := range []string{"turn.started", "turn.finished"} {
		seq := i + 1
		if strconv.Itoa(seq) <= r.Header.Get("Last-Event-ID") {
			continue
		}
		_, _ = fmt.Fprintf(w, "id: %d\ndata: {\"seq\":%d,\"type\":%q,\"at\":%q}\n\n", seq, seq, typ, at)
	}
}

// lastAsked is the member and role of the last question.
func (a *stubAgent) lastAsked() [2]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.asked
}

// public is a conversation without the stub's own fields.
func public(c map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range c {
		if k != "owner" {
			out[k] = v
		}
	}
	return out
}

func refuse(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"code": code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// agentMissing is a turn, tool call, document or skill the stub agent
// does not know, agentConflict one in a state that refuses the call, and
// agentForbidden a skill another member made.
var (
	agentMissing   = uuid.MustParse("0193e000-0000-7000-8000-00000000beef")
	agentConflict  = uuid.MustParse("0193e000-0000-7000-8000-00000000c0f1")
	agentForbidden = uuid.MustParse("0193e000-0000-7000-8000-00000000f0bd")
)

// runs serves the agent's paths of a turn's run, its tool calls, its
// documents and the skills: agentDown fails, agentMissing is not found,
// agentConflict, agentForbidden and the slug "taken" are refused, the
// answer "wrong" and the skill named "invalid" are bad requests, anything
// else answers.
func (*stubAgent) runs(mux *http.ServeMux) {
	known := func(w http.ResponseWriter, r *http.Request, conflict string) bool {
		switch uuid.MustParse(r.PathValue("id")) {
		case agentDown:
			refuse(w, http.StatusInternalServerError, "internal")
		case agentMissing:
			refuse(w, http.StatusNotFound, "not_found")
		case agentConflict:
			refuse(w, http.StatusConflict, conflict)
		case agentForbidden:
			refuse(w, http.StatusForbidden, "forbidden")
		default:
			return true
		}
		return false
	}
	mux.HandleFunc("GET /turns/{id}", func(w http.ResponseWriter, r *http.Request) {
		if known(w, r, "") {
			writeJSON(w, http.StatusOK, turnView(r.PathValue("id"), uuid.NewV7().String(), r.Header.Get("X-Console-Member"), "?",
				map[string]any{"id": "console-agent", "name": "Console assistant"}))
		}
	})
	for c, answer := range map[string]struct {
		status int
		body   map[string]any
	}{
		"pause": {http.StatusAccepted, nil}, "stop": {http.StatusAccepted, nil},
		"continue": {http.StatusAccepted, map[string]any{"turn_id": true}},
		"end":      {http.StatusOK, map[string]any{"status": "CANCELLED"}},
	} {
		mux.HandleFunc("POST /turns/{id}/"+c, func(w http.ResponseWriter, r *http.Request) {
			if !known(w, r, "turn_state") {
				return
			}
			body := map[string]any{}
			for k, v := range answer.body {
				body[k] = v
				if k == "turn_id" {
					body[k] = r.PathValue("id")
				}
			}
			writeJSON(w, answer.status, body)
		})
	}
	mux.HandleFunc("POST /turns/{id}/input", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Answers map[string]any `json:"answers"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !known(w, r, "turn_state") {
			return
		}
		if in.Answers["range"] == "wrong" {
			refuse(w, http.StatusBadRequest, "bad_request")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"turn_id": r.PathValue("id")})
	})
	mux.HandleFunc("POST /turns/{id}/steer", func(w http.ResponseWriter, r *http.Request) {
		if known(w, r, "turn_state") {
			writeJSON(w, http.StatusAccepted, map[string]any{"steer_id": uuid.NewV7(), "resumed": false})
		}
	})
	mux.HandleFunc("GET /tool-calls/{id}", func(w http.ResponseWriter, r *http.Request) {
		if known(w, r, "") {
			writeJSON(w, http.StatusOK, map[string]any{
				"id": r.PathValue("id"), "turn_id": uuid.NewV7(), "tool": "ticket_get", "status": "success",
				"input": map[string]any{"id": uuid.NewV7()}, "output": map[string]any{"status": "open"}, "output_spilled": false,
				"attempts": 1, "duration_ms": 120, "timing": map[string]any{"queued": 1, "server": 100, "db": nil, "return": 4},
				"started_at": time.Now().UTC().Format(time.RFC3339), "finished_at": time.Now().UTC().Format(time.RFC3339),
			})
		}
	})
	mux.HandleFunc("GET /documents/{id}", func(w http.ResponseWriter, r *http.Request) {
		if known(w, r, "") {
			at := time.Now().UTC().Format(time.RFC3339)
			version := func(n int) map[string]any {
				return map[string]any{
					"version": n, "title": "Báo cáo tuần", "sections": []map[string]any{{"type": "markdown", "markdown": "# Tuần"}},
					"created_at": at,
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id": r.PathValue("id"), "conversation_id": uuid.NewV7(), "title": "Báo cáo tuần", "latest_version": 2,
				"created_at": at, "updated_at": at, "versions": []map[string]any{version(2), version(1)},
			})
		}
	})
	skill := func(id, slug string, r *http.Request) map[string]any {
		return map[string]any{
			"id": id, "created_by": r.Header.Get("X-Console-Member"), "name": "Weekly", "slug": slug, "description": "",
			"prompt": "Report on {{week}}", "variables": []map[string]any{{"name": "week"}},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		}
	}
	mux.HandleFunc("GET /skills", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []map[string]any{skill(uuid.NewV7().String(), "weekly", r)})
	})
	mux.HandleFunc("GET /skills/{id}", func(w http.ResponseWriter, r *http.Request) {
		if known(w, r, "") {
			writeJSON(w, http.StatusOK, skill(r.PathValue("id"), "weekly", r))
		}
	})
	mux.HandleFunc("POST /skills", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		switch {
		case in.Slug == "taken":
			refuse(w, http.StatusConflict, "skill_exists")
		case in.Slug == "down":
			refuse(w, http.StatusInternalServerError, "internal")
		case in.Name == "invalid":
			refuse(w, http.StatusBadRequest, "bad_request")
		default:
			writeJSON(w, http.StatusCreated, skill(uuid.NewV7().String(), in.Slug, r))
		}
	})
	mux.HandleFunc("PATCH /skills/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !known(w, r, "skill_exists") {
			return
		}
		if in.Name == "invalid" {
			refuse(w, http.StatusBadRequest, "bad_request")
			return
		}
		writeJSON(w, http.StatusOK, skill(r.PathValue("id"), "weekly", r))
	})
	mux.HandleFunc("DELETE /skills/{id}", func(w http.ResponseWriter, r *http.Request) {
		if known(w, r, "") {
			w.WriteHeader(http.StatusNoContent)
		}
	})
}
