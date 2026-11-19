package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"
)

const (
	// agentServiceToken and agentMCPToken are the e2e server's
	// AGENT_SERVICE_TOKEN and AGENT_MCP_TOKEN.
	agentServiceToken = "e2e-agent-service-token-0123456789abcdef"
	agentMCPToken     = "e2e-agent-mcp-token-0123456789abcdefghij"
)

// stubAgent stands in for the agent service's POST /runs as
// shopnexus/agent serves it: it refuses a request without the service
// token, and streams NDJSON lines by the question. "down" is refused with
// 503; "ask" waits for answers; "pause" pauses; "break" ends the stream
// without an end line; "hold" polls the server's control endpoint, reads
// each steer and pauses at a stop; any other question reads a ticket,
// writes a document twice and answers with one source. A resumed segment
// answers, but one of a question ending in "-down" is refused with 503.
type stubAgent struct {
	srv *httptest.Server
	mu  sync.Mutex
	// asked is the last member and role a run came with, configs the
	// aliases of the agents the runs went to, answers the answers text of
	// each resumed segment, and made the tool call and document of each
	// turn, and mentions the records each run was told of.
	asked    [2]string
	configs  []string
	answers  []string
	made     map[string][2]string
	mentions map[string][]string
}

type stubRun struct {
	TurnID       string `json:"turn_id"`
	Mode         string `json:"mode"`
	Question     string `json:"question"`
	ControlAfter int64  `json:"control_after"`
	Answers      string `json:"answers"`
	Member       struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"member"`
	AgentConfig struct {
		Alias string `json:"alias"`
	} `json:"agent_config"`
	Mentions []struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"mentions"`
}

func newAgent() *stubAgent {
	a := &stubAgent{made: map[string][2]string{}, mentions: map[string][]string{}}
	a.srv = httptest.NewServer(http.HandlerFunc(a.run))
	return a
}

func (a *stubAgent) url() string { return a.srv.URL }

// lastAsked is the member and role of the last run.
func (a *stubAgent) lastAsked() [2]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.asked
}

// mentionsOf is the records the last run of turn was told of, as kind:id.
func (a *stubAgent) mentionsOf(turn string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mentions[turn]
}

// madeBy is the tool call and the document turn's run made.
func (a *stubAgent) madeBy(turn string) [2]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.made[turn]
}

func (a *stubAgent) run(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/runs" || r.Header.Get("Authorization") != "Bearer "+agentServiceToken {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "unauthorized"})
		return
	}
	var in stubRun
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "bad_request"})
		return
	}
	a.mu.Lock()
	a.asked = [2]string{in.Member.ID, in.Member.Role}
	a.configs = append(a.configs, in.AgentConfig.Alias)
	a.mentions[in.TurnID] = nil
	for _, m := range in.Mentions {
		a.mentions[in.TurnID] = append(a.mentions[in.TurnID], m.Kind+":"+m.ID)
	}
	if in.Mode == "resume" {
		a.answers = append(a.answers, in.Answers)
	}
	a.mu.Unlock()
	question := strings.TrimSpace(in.Question)
	if (in.Mode == "new" && question == "down") || (in.Mode == "resume" && strings.HasSuffix(question, "-down")) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "agent_unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	send := func(lines ...any) {
		for _, l := range lines {
			b, _ := json.Marshal(l)
			_, _ = w.Write(append(b, '\n'))
		}
		w.(http.Flusher).Flush()
	}
	event := func(typ string, body map[string]any) map[string]any {
		return map[string]any{"kind": "event", "type": typ, "body": body}
	}
	send(event("turn.started", map[string]any{"agent": map[string]any{"id": "a", "name": "Console assistant"}, "deadline_s": 60}))
	if in.Mode == "resume" {
		send(end("SUCCEEDED", nil))
		return
	}
	switch strings.TrimSuffix(question, "-down") {
	case "ask":
		send(end("AWAITING_INPUT", []map[string]any{
			{"id": "range", "label": "Khoảng", "kind": "single", "options": []map[string]string{{"id": "week", "label": "Tuần này"}}},
			{"id": "cities", "label": "Thành phố", "kind": "multi", "options": []map[string]string{{"id": "hn", "label": "Hà Nội"}}},
		}))
	case "pause":
		send(end("PAUSED", nil))
	case "break":
		send(event("step.started", map[string]any{"step_id": "s1", "title": "Đọc"}))
	case "hold":
		a.hold(r, in, send, event)
	default:
		call, doc := uuid.NewV7().String(), uuid.NewV7().String()
		a.mu.Lock()
		a.made[in.TurnID] = [2]string{call, doc}
		a.mu.Unlock()
		tool := func(status string, output any) map[string]any {
			return map[string]any{
				"kind": "tool_call", "id": call, "tool_use_id": "tu1", "tool": "ticket_get", "label": "Ticket", "source": "mcp",
				"input": map[string]any{"id": "x"}, "status": status, "output": output, "content": nil, "output_spilled": false,
				"latency_ms": 120, "attempts": 2, "timing": map[string]any{"queued": 1, "server": 100, "db": nil, "return": 4},
				"attempt_errors": []map[string]any{{"attempt": 1, "error": "timeout", "at": time.Now().UTC().Format(time.RFC3339)}},
				"finished_at":    nil,
			}
		}
		document := func(version int) map[string]any {
			return map[string]any{
				"kind": "document", "document_id": doc, "version": version, "title": "Báo cáo tuần",
				"sections": []map[string]any{{"type": "markdown", "markdown": "# Tuần"}},
			}
		}
		send(tool("running", nil), tool("success", `{"status":"open"}`), document(1), document(2), end("SUCCEEDED", nil))
	}
}

// hold polls the control endpoint after the run's control_after, reads
// each steer with steer.read, and pauses at the first stop.
func (*stubAgent) hold(r *http.Request, in stubRun, send func(...any), event func(string, map[string]any) map[string]any) {
	after := in.ControlAfter
	base := strings.TrimSuffix(mcpURL, "/mcp")
	for range 100 {
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet,
			fmt.Sprintf("%s/agent/turns/%s/control?after=%d", base, in.TurnID, after), http.NoBody)
		req.Header.Set("Authorization", "Bearer "+agentMCPToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		var got struct {
			Items []struct {
				Seq     int64  `json:"seq"`
				Kind    string `json:"kind"`
				SteerID string `json:"steer_id"`
			} `json:"items"`
			Running bool `json:"running"`
		}
		_ = json.NewDecoder(res.Body).Decode(&got)
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		for _, it := range got.Items {
			after = it.Seq
			switch it.Kind {
			case "steer":
				send(event("steer.read", map[string]any{"steer_id": it.SteerID, "step_id": nil, "step_number": 1}))
			case "stop":
				send(end("PAUSED", nil))
				return
			default:
			}
		}
		if !got.Running {
			send(end("PAUSED", nil))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// end is a run's end line in status, with the questions of ask_user.
func end(status string, questions []map[string]any) map[string]any {
	state := map[string]any{
		"v": 1, "question": "?", "messages": []any{}, "sources": []any{}, "steps": []any{}, "wrote": true,
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "cost_usd": 0.001},
	}
	line := map[string]any{
		"kind": "end", "status": status, "error_code": nil, "answer": nil, "blocks": []any{}, "sources": []any{},
		"usage": map[string]any{
			"input_tokens": 10, "output_tokens": 5, "cache_read_tokens": 0, "cache_write_tokens": 0, "cost_usd": 0.001,
			"first_token_ms": 300, "provider": "anthropic",
		},
		"latency_ms": 4200, "wrote": true, "state": state,
	}
	switch status {
	case "SUCCEEDED":
		sources := []any{map[string]any{
			"n": 1, "kind": "ticket", "record_id": "t1", "label": "Ticket", "call_id": uuid.NewV7(), "tool": "ticket_get",
		}}
		line["answer"], line["sources"] = "Đã đọc [1].", sources
		line["blocks"] = []map[string]any{{"type": "text", "markdown": "Đã đọc [1]."}}
		line["event"] = map[string]any{"type": "turn.finished", "body": map[string]any{
			"status": "SUCCEEDED", "outcome": nil, "continuable": false, "sources": sources,
		}}
	case "PAUSED":
		line["event"] = map[string]any{"type": "turn.paused", "body": map[string]any{"steps_done": 0, "reason": "member"}}
	case "AWAITING_INPUT":
		state["pending_ask"] = questions
		line["event"] = map[string]any{"type": "clarify", "body": map[string]any{"questions": questions}}
	default:
	}
	return line
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// askAndWait opens a conversation of member's own, asks question and
// reads the turn's events until the stream ends.
func askAndWait(t *testing.T, member consoleSession, question string) (conversation, turn string) {
	t.Helper()
	c := expect[agentConversation](t, consoleCall(t, member, "POST", "/v1/console/agent/conversations", map[string]any{}), http.StatusOK)
	started := expect[agentTurnStarted](t, consoleCall(t, member, "POST", "/v1/console/agent/conversations/"+c.ID+"/turns",
		map[string]string{"text": question}), http.StatusAccepted)
	waitTurn(t, member, started.TurnID)
	return c.ID, started.TurnID
}

// waitTurn reads a turn's events until the stream ends, at a stop event.
func waitTurn(t *testing.T, member consoleSession, turn string) string {
	t.Helper()
	res := consoleCall(t, member, "GET", "/v1/console/agent/turns/"+turn+"/events", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("events of %s: %d %s", turn, res.Status, res.Body)
	}
	return string(res.Body)
}
