package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"slow":   {http.StatusGatewayTimeout, "agent_timeout"},
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
	// asked is the last member and role a question came with.
	asked [2]string
}

func newAgent() *stubAgent {
	a := &stubAgent{conversations: map[string]map[string]any{}, turns: map[string][]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /conversations", a.list)
	mux.HandleFunc("POST /conversations", a.open)
	mux.HandleFunc("GET /conversations/{id}", a.get)
	mux.HandleFunc("POST /conversations/{id}/turns", a.ask)
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
		Text string `json:"text"`
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
	sources := []map[string]any{{"tool": "ticket_get", "label": "Ticket", "ids": []string{id}}}
	a.turns[id] = append(a.turns[id], map[string]any{
		"id": turn, "member_id": r.Header.Get("X-Console-Member"), "text": in.Text, "answer": "Đã đọc.", "sources": sources,
		"status": "SUCCEEDED", "created_at": time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]any{"conversation_id": id, "turn_id": turn, "answer": "Đã đọc.", "sources": sources})
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
