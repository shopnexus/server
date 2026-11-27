package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
)

// run is a run of a turn about to start: the turn running, its agent and
// route, its token, and the messages that join the history first.
type run struct {
	turn         repository.Turn
	conversation repository.Conversation
	agent        repository.Agent
	route        gatewayRoute
	token        string
	first        []json.RawMessage
	reported     []uuid.UUID
	by           uuid.UUID
	resumed      bool
}

// runtimeTurn is the agent runtime's POST /v1/turns body (its
// runtime.TurnRequest).
type runtimeTurn struct {
	ConversationID string            `json:"conversation_id"`
	TurnID         string            `json:"turn_id"`
	Instructions   string            `json:"instructions"`
	History        []json.RawMessage `json:"history"`
	Token          string            `json:"token"`
	Model          string            `json:"model"`
	ModelKey       string            `json:"model_key"`
	Deadline       time.Time         `json:"deadline"`
}

// controlRequest is the body of the runtime's pause, stop and steer.
type controlRequest struct {
	TurnID  string `json:"turn_id"`
	Content string `json:"content,omitempty"`
	Context string `json:"context,omitempty"`
}

// runtimeEvent is one SSE event of a run (the runtime's agent.Event), and
// runtimeResult its last, "result".
type runtimeEvent struct {
	Type       string            `json:"type"`
	Delta      string            `json:"delta"`
	ToolCallID string            `json:"tool_call_id"`
	Tool       string            `json:"tool"`
	Input      string            `json:"input"`
	Output     string            `json:"output"`
	IsError    bool              `json:"is_error"`
	Err        string            `json:"err"`
	Messages   []json.RawMessage `json:"messages"`
}

type runtimeResult struct {
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"usage"`
	Stop           string `json:"stop"`
	Error          string `json:"error"`
	WorkspaceError string `json:"workspace_error"`
}

// start sends a run to the runtime over the conversation's history and
// r.first. Once the runtime takes it, r.first joins the history and the
// run's stream is followed in the background until it ends; an error means
// the run did not start.
func (s *Agent) start(ctx context.Context, r run) error {
	stored, err := s.d.Conversations.Messages(ctx, r.conversation.ID)
	if err != nil {
		return fmt.Errorf("read the history: %w", err)
	}
	history := make([]json.RawMessage, 0, len(stored)+len(r.first))
	for _, m := range stored {
		history = append(history, m.Body)
	}
	history = append(history, r.first...)
	body, err := json.Marshal(runtimeTurn{
		ConversationID: r.conversation.ID.String(), TurnID: r.turn.ID.String(), Instructions: instructionsOf(r.agent, s.d.Ops.Granted(nonNil(r.turn.Capabilities), r.turn.Role)),
		History: history, Token: r.token, Model: r.route.model, ModelKey: r.route.key, Deadline: r.turn.DeadlineAt,
	})
	if err != nil {
		return fmt.Errorf("encode the run: %w", err)
	}
	// The run outlives the request that started it, until StopRuns.
	runCtx, cancel := context.WithCancel(s.runsCtx)
	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, s.d.RuntimeURL+"/v1/turns", bytes.NewReader(body))
	if err != nil {
		cancel()
		return fmt.Errorf("build the run request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.d.RuntimeKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.d.HTTP.Do(req)
	if err != nil {
		cancel()
		return domain.ErrUnavailable.WithCause(err)
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode == http.StatusConflict {
			return domain.ErrTurnInProgress
		}
		return domain.ErrUnavailable.WithCause(fmt.Errorf("agent runtime: %s: %s", resp.Status, bytes.TrimSpace(msg)))
	}
	now := s.d.Now().UTC()
	if _, err = s.d.Conversations.AppendMessages(ctx, r.turn.ID, r.turn.RunStartedAt, r.first, now); err == nil {
		err = s.d.Conversations.MarkReported(ctx, r.reported, now)
	}
	if err != nil {
		// The runtime reads a message the history lacks: stop the run, so
		// the next one starts from what is stored.
		s.d.Log.ErrorContext(ctx, "agent: store a run's first messages", "turn_id", r.turn.ID, "err", err)
		go s.stopRun(r.turn)
	}
	typ, startBody := "turn.started", map[string]any{
		"agent": TurnAgent{ID: r.agent.ID.String(), Name: r.agent.Name}, "model": r.route.model,
	}
	if r.resumed {
		typ, startBody = "turn.resumed", map[string]any{"by": r.by}
	}
	sb, _ := json.Marshal(startBody)
	if _, err = s.d.Conversations.AppendEvent(ctx, r.turn.ID, typ, sb, now); err != nil {
		s.d.Log.WarnContext(ctx, "agent: note a run's start", "turn_id", r.turn.ID, "err", err)
	}
	s.runs.Go(func() {
		defer cancel()
		defer func() { _ = resp.Body.Close() }()
		s.consume(runCtx, r.turn, resp.Body)
	})
	return nil
}

// stopRun stops a run that cannot go on, through the runtime.
func (s *Agent) stopRun(t repository.Turn) {
	ctx, cancel := context.WithTimeout(s.runsCtx, 10*time.Second)
	defer cancel()
	if err := s.runtimeControl(ctx, t.ConversationID, "stop", controlRequest{TurnID: t.ID.String()}); err != nil {
		s.d.Log.WarnContext(ctx, "agent: stop a run", "turn_id", t.ID, "err", err)
	}
}

// StopRuns cancels the runs this replica follows, which ends them on the
// runtime as their streams close, and waits for their turns to be closed.
func (s *Agent) StopRuns() {
	s.stopRuns()
	s.runs.Wait()
}

// runtimeControl posts a control to the runtime for the turn running in a
// conversation. The runtime's 404 means no such turn runs there any more.
func (s *Agent) runtimeControl(ctx context.Context, conversation uuid.UUID, what string, body controlRequest) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode the control: %w", err)
	}
	u := s.d.RuntimeURL + "/v1/turns/" + url.PathEscape(conversation.String()) + "/" + what
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build the control: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.d.RuntimeKey)
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := s.d.HTTP.Do(req.WithContext(ctx))
	if err != nil {
		return domain.ErrUnavailable.WithCause(err)
	}
	defer func() { _ = resp.Body.Close() }()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return domain.ErrTurnState
	case resp.StatusCode >= 300:
		return domain.ErrUnavailable.WithCause(fmt.Errorf("agent runtime %s: %s: %s", what, resp.Status, bytes.TrimSpace(msg)))
	default:
		return nil
	}
}

// deltaEvery is how long text deltas gather before they are stored as one
// event, so a fast model does not write a row per token.
const deltaEvery = 200 * time.Millisecond

// consume reads a run's stream, stores its steps' messages in the history
// and its events for the console, and closes the turn with the result. A
// stream that ends without one fails the turn.
func (s *Agent) consume(ctx context.Context, t repository.Turn, stream io.Reader) {
	// The writes outlive a cancelled run, so its end is stored.
	store := context.WithoutCancel(ctx)
	emit := func(typ string, body any) {
		b, _ := json.Marshal(body)
		if _, err := s.d.Conversations.AppendEvent(store, t.ID, typ, b, s.d.Now().UTC()); err != nil {
			s.d.Log.WarnContext(store, "agent: store a run's event", "turn_id", t.ID, "type", typ, "err", err)
		}
	}
	var pending strings.Builder
	var pendingType string
	var pendingSince time.Time
	flush := func() {
		if pending.Len() > 0 {
			emit(pendingType, map[string]string{"delta": pending.String()})
			pending.Reset()
		}
	}
	var result *runtimeResult
	err := readSSE(stream, func(event string, data []byte) error {
		if event == "result" {
			flush()
			result = &runtimeResult{}
			return json.Unmarshal(data, result)
		}
		var e runtimeEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("decode a %s event: %w", event, err)
		}
		switch e.Type {
		case "text", "reasoning":
			typ := "text.delta"
			if e.Type == "reasoning" {
				typ = "reasoning.delta"
			}
			if typ != pendingType {
				flush()
				pendingType, pendingSince = typ, time.Now()
			}
			pending.WriteString(e.Delta)
			if time.Since(pendingSince) >= deltaEvery {
				flush()
				pendingSince = time.Now()
			}
			return nil
		}
		flush()
		switch e.Type {
		case "tool_call":
			emit("tool.started", map[string]string{"call_id": e.ToolCallID, "tool": e.Tool, "input": e.Input})
		case "tool_result":
			emit("tool.finished", map[string]any{"call_id": e.ToolCallID, "tool": e.Tool, "output": e.Output, "is_error": e.IsError})
		case "step":
			ok, err := s.d.Conversations.AppendMessages(store, t.ID, t.RunStartedAt, e.Messages, s.d.Now().UTC())
			if err != nil {
				return fmt.Errorf("store a step's messages: %w", err)
			}
			if !ok {
				return errors.New("the turn's run was closed while it ran")
			}
			emit("step", map[string]any{"messages": e.Messages})
		case "step_failed":
			emit("step.failed", map[string]string{"error": e.Err})
		default:
			// An event a newer runtime sends that the console does not show.
		}
		return nil
	})
	end := repository.RunEnd{TurnID: t.ID, RunStartedAt: t.RunStartedAt, At: s.d.Now().UTC()}
	switch {
	case result != nil:
		end.Stop, end.Error = result.Stop, result.Error
		end.Usage = repository.Usage{
			InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, CachedTokens: result.Usage.CachedTokens,
		}
		if result.WorkspaceError != "" {
			s.d.Log.WarnContext(store, "agent: the runtime did not save a workspace", "turn_id", t.ID, "err", result.WorkspaceError)
		}
	case ctx.Err() != nil:
		end.Error = "the server stopped while the turn ran"
	case err != nil:
		end.Error = "the agent runtime's stream broke: " + err.Error()
	default:
		end.Error = "the agent runtime's stream ended without a result"
	}
	if err != nil && result != nil {
		s.d.Log.WarnContext(store, "agent: read a run's stream", "turn_id", t.ID, "err", err)
	}
	end.Status = domain.StatusOf(end.Stop, end.Error)
	s.finish(store, end)
}

// finish closes a run with turn.paused or turn.finished, once.
func (s *Agent) finish(ctx context.Context, end repository.RunEnd) {
	usage := Usage(end.Usage)
	typ, body := "turn.finished", map[string]any{"status": end.Status, "stop": end.Stop, "error": end.Error, "usage": usage}
	if end.Status == domain.TurnPaused {
		typ, body = "turn.paused", map[string]any{"usage": usage}
	}
	b, _ := json.Marshal(body)
	if _, err := s.d.Conversations.FinishRun(ctx, end, typ, b); err != nil {
		s.d.Log.ErrorContext(ctx, "agent: close a run", "turn_id", end.TurnID, "err", err)
	}
}

// maxEventLine bounds one SSE line: a step's messages carry command
// outputs, which the runtime already caps.
const maxEventLine = 16 << 20

// readSSE calls on with each event of an SSE stream until it ends.
func readSSE(r io.Reader, on func(event string, data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxEventLine)
	event := ""
	var data bytes.Buffer
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case len(line) == 0:
			if data.Len() > 0 {
				if err := on(event, data.Bytes()); err != nil {
					return err
				}
			}
			event = ""
			data.Reset()
		case bytes.HasPrefix(line, []byte("event:")):
			event = strings.TrimSpace(string(line[len("event:"):]))
		case bytes.HasPrefix(line, []byte("data:")):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.Write(bytes.TrimPrefix(line[len("data:"):], []byte(" ")))
		default:
			// A comment, or a field the server does not read.
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read the stream: %w", err)
	}
	return nil
}
