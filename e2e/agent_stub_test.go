package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"time"
)

// agentKey is the key the server calls the agent runtime with.
const agentKey = "e2e-agent-key-0123456789abcdefghijklmnop"

// stubAgent stands in for the agent runtime. A turn's last message with
// content is its script:
//
//	down   the runtime refuses the turn with 503
//	budget the model gateway refuses the first call: the agent's budget is
//	       spent
//	hold   the turn runs until a pause or a stop, keeping the steers it
//	       gets, so a test calls the server with its token meanwhile
//
// Any other script, and a turn the stub ran before (a resumed one), gets
// the answer "Đã xong.". A conversation the stub refuses gets 503 for a
// turn and 500 for a control.
type stubAgent struct {
	srv *httptest.Server

	mu      sync.Mutex
	turns   map[string]*stubTurn // by turn id
	running map[string]*stubTurn // by conversation id
	refused map[string]bool      // by conversation id
}

// stubTurn is what the last run of a turn received.
type stubTurn struct {
	Instructions string
	History      []agentMessage
	Token        string
	Model        string
	ModelKey     string
	Steers       []stubControl

	id      string
	runs    int
	control chan stubControl
}

type stubControl struct {
	What    string `json:"-"`
	TurnID  string `json:"turn_id"`
	Content string `json:"content"`
	Context string `json:"context"`
}

// agentMessage is a message of a conversation's history.
type agentMessage struct {
	Role    string `json:"role"`
	Context string `json:"context,omitempty"`
	Content string `json:"content,omitempty"`
}

func newAgent() *stubAgent {
	a := &stubAgent{turns: map[string]*stubTurn{}, running: map[string]*stubTurn{}, refused: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/turns", a.serveTurn)
	mux.HandleFunc("POST /v1/turns/{conversation}/{what}", a.serveControl)
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+agentKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return a
}

func (a *stubAgent) url() string { return a.srv.URL }

// ran reads what the last run of a turn received.
func (a *stubAgent) ran(id string) stubTurn {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t, ok := a.turns[id]; ok {
		return *t
	}
	return stubTurn{}
}

// holding waits until turn id holds, so its token works and a control
// finds it.
func (a *stubAgent) holding(id string) bool {
	for range 250 {
		a.mu.Lock()
		t, ok := a.turns[id]
		held := ok && t.control != nil
		a.mu.Unlock()
		if held {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// refuse makes the runtime fail conversation's turns and controls, or
// take them again.
func (a *stubAgent) refuse(conversation string, refused bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refused[conversation] = refused
}

func (a *stubAgent) serveTurn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConversationID string            `json:"conversation_id"`
		TurnID         string            `json:"turn_id"`
		Instructions   string            `json:"instructions"`
		History        []json.RawMessage `json:"history"`
		Token          string            `json:"token"`
		Model          string            `json:"model"`
		ModelKey       string            `json:"model_key"`
		Deadline       time.Time         `json:"deadline"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TurnID == "" || req.Token == "" || req.Deadline.IsZero() {
		http.Error(w, "bad turn", http.StatusBadRequest)
		return
	}
	history := make([]agentMessage, 0, len(req.History))
	for _, raw := range req.History {
		var m agentMessage
		if json.Unmarshal(raw, &m) != nil {
			http.Error(w, "bad message", http.StatusBadRequest)
			return
		}
		history = append(history, m)
	}
	script := ""
	for _, m := range slices.Backward(history) {
		if m.Role == "user" && m.Content != "" {
			script = strings.TrimSpace(m.Content)
			break
		}
	}
	a.mu.Lock()
	if script == "down" || a.refused[req.ConversationID] {
		a.mu.Unlock()
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	if _, busy := a.running[req.ConversationID]; busy {
		a.mu.Unlock()
		http.Error(w, "busy", http.StatusConflict)
		return
	}
	t := a.turns[req.TurnID]
	if t == nil {
		t = &stubTurn{id: req.TurnID}
		a.turns[req.TurnID] = t
	}
	t.runs++
	if t.runs > 1 {
		script = ""
	}
	t.Instructions, t.History, t.Token, t.Model, t.ModelKey = req.Instructions, history, req.Token, req.Model, req.ModelKey
	a.running[req.ConversationID] = t
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.running, req.ConversationID)
		t.control = nil
		a.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		_ = http.NewResponseController(w).Flush()
	}
	result := func(stop string, messages ...agentMessage) {
		send("result", map[string]any{
			"messages": messages, "stop": stop,
			"usage": map[string]int{"input_tokens": 100, "output_tokens": 10, "cached_tokens": 40},
		})
	}
	if script == "budget" {
		send("step_failed", map[string]string{"type": "step_failed", "err": "openai: 402 Payment Required"})
		result("budget")
		return
	}
	if script == "hold" {
		send("tool_call", map[string]string{"type": "tool_call", "tool_call_id": "c1", "tool": "shell", "input": "shopnexus getAccount"})
		send("tool_result", map[string]string{"type": "tool_result", "tool_call_id": "c1", "tool": "shell", "output": "{}"})
		control := make(chan stubControl, 4)
		a.mu.Lock()
		t.control = control
		a.mu.Unlock()
		for {
			select {
			case c := <-control:
				if c.What == "steer" {
					a.mu.Lock()
					t.Steers = append(t.Steers, c)
					a.mu.Unlock()
					continue
				}
				if c.What == "pause" {
					result("paused")
				} else {
					result("stopped")
				}
			case <-time.After(time.Until(req.Deadline)):
				result("deadline")
			case <-r.Context().Done():
			}
			return
		}
	}
	send("text", map[string]string{"type": "text", "delta": "Đã "})
	send("text", map[string]string{"type": "text", "delta": "xong."})
	answer := agentMessage{Role: "assistant", Content: "Đã xong."}
	send("step", map[string]any{"type": "step", "messages": []agentMessage{answer}})
	result("done", answer)
}

// serveControl is the runtime's pause, stop and steer of the turn running
// in a conversation.
func (a *stubAgent) serveControl(w http.ResponseWriter, r *http.Request) {
	var c stubControl
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "bad control", http.StatusBadRequest)
		return
	}
	c.What = r.PathValue("what")
	conversation := r.PathValue("conversation")
	a.mu.Lock()
	t, ok := a.running[conversation]
	refused := a.refused[conversation]
	var control chan stubControl
	if ok {
		control = t.control
	}
	a.mu.Unlock()
	switch {
	case refused:
		http.Error(w, "down", http.StatusInternalServerError)
	case !ok || control == nil || t.id != c.TurnID || (c.What != "pause" && c.What != "stop" && c.What != "steer"):
		http.Error(w, "no turn runs", http.StatusNotFound)
	default:
		control <- c
		w.WriteHeader(http.StatusNoContent)
	}
}
