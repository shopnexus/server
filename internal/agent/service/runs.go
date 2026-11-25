package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/authn"
)

const (
	// openWait bounds the wait for POST /runs to answer with its status.
	openWait = 10 * time.Second
	// readGrace is how long past a segment's deadline its stream is read.
	readGrace = 60 * time.Second
	// maxLine bounds one line of a run's stream, and maxRefusal the body
	// of a refusal.
	maxLine    = 16 << 20
	maxRefusal = 4 << 10
	// historyTurns is how many prior turns a new run replays.
	historyTurns = 20
)

// runRequest is the body of the agent's POST /runs. History and Steers
// are any so that an empty list is sent as [] and an unset one not at all.
type runRequest struct {
	TurnID         uuid.UUID     `json:"turn_id"`
	ConversationID uuid.UUID     `json:"conversation_id"`
	Member         runMember     `json:"member"`
	Mode           string        `json:"mode"`
	Question       string        `json:"question"`
	Subject        *runSubject   `json:"subject,omitempty"`
	Mentions       []runSubject  `json:"mentions"`
	AgentConfig    *AgentConfig  `json:"agent_config,omitempty"`
	Skill          *runSkill     `json:"skill,omitempty"`
	History        any           `json:"history,omitempty"`
	State          any           `json:"state,omitempty"`
	Steers         any           `json:"steers,omitempty"`
	Answers        string        `json:"answers,omitempty"`
	Documents      []runDocument `json:"documents"`
	ControlAfter   int64         `json:"control_after"`
}

type runMember struct {
	ID   uuid.UUID `json:"id"`
	Role string    `json:"role"`
}

type runSubject struct {
	Kind string    `json:"kind"`
	ID   uuid.UUID `json:"id"`
}

type runSkill struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Prompt string    `json:"prompt"`
}

type runSteer struct {
	SteerID uuid.UUID `json:"steer_id"`
	Text    string    `json:"text"`
}

type runDocument struct {
	DocumentID    uuid.UUID `json:"document_id"`
	LatestVersion int       `json:"latest_version"`
}

// historyTurn is a prior terminal turn as a new run replays it.
type historyTurn struct {
	Status   string         `json:"status"`
	Question string         `json:"question"`
	Entries  []historyEntry `json:"entries"`
	Answer   string         `json:"answer,omitempty"`
}

type historyEntry struct {
	Kind      string          `json:"kind"`
	Text      string          `json:"text,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Status    string          `json:"status,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// segment is the run segment a stream belongs to.
type segment struct {
	turn         uuid.UUID
	conversation uuid.UUID
	member       uuid.UUID
	start        time.Time
}

// newRequest is a run's request for turn t's caller, its subject, the
// records its conversation mentions and its documents. A mention goes
// without its label: the agent's prompt takes only values the server owns.
func newRequest(t repository.Turn, caller authn.ConsoleMember, mode string, docs []repository.DocumentRef,
	mentions []domain.Attachment,
) runRequest {
	req := runRequest{
		TurnID: t.ID, ConversationID: t.ConversationID, Member: runMember{ID: caller.MemberID, Role: caller.Role},
		Mode: mode, Question: t.Question, Mentions: make([]runSubject, 0, len(mentions)), Documents: make([]runDocument, 0, len(docs)),
	}
	if t.SubjectKind != "" && t.SubjectID != nil {
		req.Subject = &runSubject{Kind: t.SubjectKind, ID: *t.SubjectID}
	}
	for _, m := range mentions {
		req.Mentions = append(req.Mentions, runSubject{Kind: m.Kind, ID: m.ID})
	}
	for _, d := range docs {
		req.Documents = append(req.Documents, runDocument{DocumentID: d.ID, LatestVersion: d.LatestVersion})
	}
	return req
}

// historyOf is the history a new run replays: each tool call with its
// content, or its output as text when the agent kept none.
func historyOf(turns []repository.History) []historyTurn {
	out := make([]historyTurn, 0, len(turns))
	for _, h := range turns {
		ht := historyTurn{Status: strings.ToUpper(h.Status), Question: h.Question, Answer: h.Answer, Entries: []historyEntry{}}
		for i := range h.Entries {
			e := &h.Entries[i]
			if e.Kind == "input" {
				ht.Entries = append(ht.Entries, historyEntry{Kind: "input", Text: e.Text})
				continue
			}
			status := "error"
			if e.Status == "success" {
				status = "success"
			}
			content := e.Content
			if content == nil {
				text := e.Text
				switch {
				case text == "" && status == "success":
					// The output was blanked after ReadsKeptFor or the account's erasure.
					text = "Result no longer kept"
				case text == "":
					text = "Tool unavailable"
				default: // the tool's own text
				}
				content, _ = json.Marshal([]map[string]string{{"text": text}})
			}
			ht.Entries = append(ht.Entries, historyEntry{
				Kind: "tool", ToolUseID: e.ToolUseID, Name: e.Tool, Input: e.Input, Status: status, Content: content,
			})
		}
		out = append(out, ht)
	}
	return out
}

// startRun posts req to the agent and waits openWait at most for its
// status. On 200 it returns the stream, read under a context that ends
// readGrace past the segment's deadline and is not the caller's; any
// other answer is domain.ErrUnavailable with the agent's code as cause.
func (s *Agent) startRun(ctx context.Context, req runRequest, deadline time.Duration) (io.ReadCloser, context.CancelFunc, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("encode the run request: %w", err)
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deadline+readGrace)
	r, err := http.NewRequestWithContext(runCtx, http.MethodPost, strings.TrimSuffix(s.d.BaseURL, "/")+"/runs", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("build the run request: %w", err)
	}
	r.Header.Set("Authorization", "Bearer "+s.d.Token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/x-ndjson")
	opened := time.AfterFunc(openWait, cancel)
	res, err := s.d.HTTP.Do(r)
	if !opened.Stop() {
		cancel()
		if res != nil {
			_ = res.Body.Close()
		}
		return nil, nil, domain.ErrUnavailable.WithCause(errors.New("the agent did not answer POST /runs in time"))
	}
	if err != nil {
		cancel()
		return nil, nil, domain.ErrUnavailable.WithCause(err)
	}
	if res.StatusCode != http.StatusOK {
		defer cancel()
		defer func() { _ = res.Body.Close() }()
		var refusal struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, maxRefusal)).Decode(&refusal)
		return nil, nil, domain.ErrUnavailable.WithCause(fmt.Errorf("POST /runs answered %d %q", res.StatusCode, refusal.Code))
	}
	return res.Body, cancel, nil
}

// runLine is one line of a run's stream; Kind says which fields it has.
type runLine struct {
	Kind string `json:"kind"`
	// event
	Type string          `json:"type"`
	Body json.RawMessage `json:"body"`
	// checkpoint and end
	State json.RawMessage `json:"state"`
	// tool_call
	ID            uuid.UUID       `json:"id"`
	ToolUseID     string          `json:"tool_use_id"`
	Tool          string          `json:"tool"`
	Label         *string         `json:"label"`
	Input         json.RawMessage `json:"input"`
	Status        string          `json:"status"`
	Output        json.RawMessage `json:"output"`
	Content       json.RawMessage `json:"content"`
	OutputSpilled *bool           `json:"output_spilled"`
	LatencyMS     *float64        `json:"latency_ms"`
	Attempts      *int            `json:"attempts"`
	AttemptErrors json.RawMessage `json:"attempt_errors"`
	Timing        json.RawMessage `json:"timing"`
	FinishedAt    *time.Time      `json:"finished_at"`
	// document
	DocumentID uuid.UUID       `json:"document_id"`
	Version    int             `json:"version"`
	Title      string          `json:"title"`
	Sections   json.RawMessage `json:"sections"`
	// end
	ErrorCode *string         `json:"error_code"`
	Answer    string          `json:"answer"`
	Blocks    json.RawMessage `json:"blocks"`
	Sources   json.RawMessage `json:"sources"`
	Usage     runUsage        `json:"usage"`
	Wrote     bool            `json:"wrote"`
	Event     *struct {
		Type string          `json:"type"`
		Body json.RawMessage `json:"body"`
	} `json:"event"`
}

type runUsage struct {
	InputTokens      float64  `json:"input_tokens"`
	OutputTokens     float64  `json:"output_tokens"`
	CacheReadTokens  float64  `json:"cache_read_tokens"`
	CacheWriteTokens float64  `json:"cache_write_tokens"`
	CostUSD          float64  `json:"cost_usd"`
	FirstTokenMS     *float64 `json:"first_token_ms"`
	Provider         string   `json:"provider"`
}

// endStatuses maps an end line's status to the turn's.
var endStatuses = map[string]string{
	"SUCCEEDED": domain.TurnSucceeded, "FAILED": domain.TurnFailed, "TIMED_OUT": domain.TurnTimedOut,
	"PAUSED": domain.TurnPaused, "AWAITING_INPUT": domain.TurnAwaitingInput,
}

// readRun applies a segment's stream line by line, each in its own
// transaction, and closes the turn as agent_stuck when the stream ends
// without an end line. It owns body and cancel.
func (s *Agent) readRun(ctx context.Context, seg segment, body io.ReadCloser, cancel context.CancelFunc) {
	defer cancel()
	defer func() { _ = body.Close() }()
	lines := bufio.NewScanner(body)
	lines.Buffer(make([]byte, 0, 64<<10), maxLine)
	ended := false
	for !ended && lines.Scan() {
		line := bytes.TrimSpace(lines.Bytes())
		if len(line) == 0 {
			continue
		}
		var err error
		if ended, err = s.applyLine(ctx, seg, line); err != nil {
			s.d.Log.WarnContext(ctx, "agent: apply a run line", "turn_id", seg.turn, "err", err)
		}
	}
	if err := lines.Err(); err != nil && !ended {
		s.d.Log.WarnContext(ctx, "agent: read a run's stream", "turn_id", seg.turn, "err", err)
	}
	if !ended {
		closeCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), openWait)
		defer stop()
		if err := s.closeStuck(closeCtx, seg.turn, seg.start); err != nil {
			s.d.Log.WarnContext(ctx, "agent: close a run whose stream broke", "turn_id", seg.turn, "err", err)
		}
	}
}

// writes are the event types that mean a run wrote something.
var writes = map[string]bool{
	"step.started": true, "step.finished": true, "tool.started": true, "tool.finished": true, "text.delta": true, "block": true,
}

// applyLine applies one line of seg's stream and says whether it was the
// end. A line for a turn that no longer runs seg changes nothing.
func (s *Agent) applyLine(ctx context.Context, seg segment, raw []byte) (bool, error) {
	var l runLine
	if err := json.Unmarshal(raw, &l); err != nil {
		return false, fmt.Errorf("decode a run line: %w", err)
	}
	now := s.d.Now()
	switch l.Kind {
	case "event":
		var steer *uuid.UUID
		if l.Type == "steer.read" {
			var b struct {
				SteerID uuid.UUID `json:"steer_id"`
			}
			if json.Unmarshal(l.Body, &b) == nil && b.SteerID != uuid.Nil() {
				steer = &b.SteerID
			}
		}
		if _, err := s.d.Runs.AppendRunEvent(ctx, seg.turn, seg.start, l.Type, l.Body, writes[l.Type], steer, now); err != nil {
			return false, fmt.Errorf("append %s: %w", l.Type, err)
		}
	case "checkpoint":
		var st struct {
			Wrote bool `json:"wrote"`
		}
		_ = json.Unmarshal(l.State, &st)
		if err := s.d.Runs.Checkpoint(ctx, seg.turn, seg.start, l.State, st.Wrote); err != nil {
			return false, fmt.Errorf("store a checkpoint: %w", err)
		}
	case "tool_call":
		if err := s.d.Runs.SaveToolCall(ctx, repository.ToolWrite{
			ID: l.ID, TurnID: seg.turn, Segment: seg.start, ToolUseID: l.ToolUseID, Tool: l.Tool, Label: l.Label, Input: l.Input,
			Output: outputOf(l.Output), Content: nullable(l.Content), OutputSpilled: l.OutputSpilled, Status: l.Status,
			LatencyMS: rounded(l.LatencyMS), Attempts: l.Attempts, AttemptErrors: nullable(l.AttemptErrors),
			Timing: nullable(l.Timing), FinishedAt: l.FinishedAt, At: now,
		}); err != nil {
			return false, fmt.Errorf("store a tool call: %w", err)
		}
	case "document":
		ok, err := s.d.Runs.WriteDocument(ctx, repository.DocumentWrite{
			ID: l.DocumentID, ConversationID: seg.conversation, TurnID: seg.turn, Segment: seg.start, MemberID: seg.member, Version: l.Version,
			Title: l.Title, Sections: l.Sections, At: now,
		})
		if err != nil {
			return false, fmt.Errorf("store a document: %w", err)
		}
		if !ok {
			s.d.Log.WarnContext(ctx, "agent: a document write was left out", "turn_id", seg.turn, "document_id", l.DocumentID,
				"version", l.Version)
		}
	case "end":
		status, known := endStatuses[l.Status]
		if !known {
			return false, fmt.Errorf("an end line with status %q", l.Status)
		}
		e := repository.RunEnd{
			TurnID: seg.turn, Segment: seg.start, Status: status, Answer: l.Answer, Blocks: nullable(l.Blocks), Sources: nullable(l.Sources),
			Usage: repository.Usage{
				InputTokens: int64(l.Usage.InputTokens), OutputTokens: int64(l.Usage.OutputTokens),
				CacheReadTokens: int64(l.Usage.CacheReadTokens), CacheWriteTokens: int64(l.Usage.CacheWriteTokens),
				CostUSD: l.Usage.CostUSD, FirstTokenMS: rounded(l.Usage.FirstTokenMS), Provider: l.Usage.Provider,
			},
			LatencyMS: l.latency(), Wrote: l.Wrote, State: nullable(l.State), At: now,
		}
		if l.ErrorCode != nil {
			e.ErrorCode = *l.ErrorCode
		}
		if l.Event != nil {
			e.EventType, e.EventBody = l.Event.Type, l.Event.Body
		}
		if _, err := s.d.Runs.FinishRun(ctx, e); err != nil {
			return false, fmt.Errorf("finish the run: %w", err)
		}
		return true, nil
	default:
	}
	return false, nil
}

func (l runLine) latency() int64 {
	if l.LatencyMS == nil {
		return 0
	}
	return max(0, int64(math.Round(*l.LatencyMS)))
}

// nullable is raw, or nil for JSON null.
func nullable(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

// outputOf is a tool call's output as agent_tool_calls keeps it: a JSON
// string as its text, other JSON as written.
func outputOf(raw json.RawMessage) *string {
	raw = nullable(raw)
	if raw == nil {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return &text
	}
	text = string(raw)
	return &text
}

func rounded(f *float64) *int64 {
	if f == nil {
		return nil
	}
	return new(max(0, int64(math.Round(*f))))
}

// closeStuck closes a turn still running the segment that started at
// start as failed with agent_stuck: its stream broke, or the sweep found
// it past its deadline.
func (s *Agent) closeStuck(ctx context.Context, id uuid.UUID, start time.Time) error {
	t, err := s.d.Runs.Turn(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the turn: %w", err)
	}
	if t.Status != domain.TurnRunning || !t.RunStartedAt.Equal(start) {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"status": "FAILED", "outcome": domain.Outcome(domain.TurnFailed, domain.FailStuck), "failure": domain.FailStuck,
		"continuable": t.Wrote, "sources": sourcesOf(t),
	})
	if err != nil {
		return fmt.Errorf("encode turn.finished: %w", err)
	}
	if _, err = s.d.Runs.CloseStuck(ctx, id, start, domain.FailStuck, domain.FailureAnswers[domain.FailStuck], body, s.d.Now()); err != nil {
		return fmt.Errorf("close the turn: %w", err)
	}
	return nil
}

// runState is what the server reads of a turn's run state.
type runState struct {
	Sources    json.RawMessage `json:"sources"`
	PendingAsk json.RawMessage `json:"pending_ask"`
}

func stateOf(t repository.Turn) runState {
	var st runState
	if t.State != nil {
		_ = json.Unmarshal(t.State, &st)
	}
	return st
}

// sourcesOf is a turn's sources: the stored ones, else its run state's.
func sourcesOf(t repository.Turn) json.RawMessage {
	if s := nullable(t.Sources); s != nil {
		return s
	}
	if s := nullable(stateOf(t).Sources); s != nil {
		return s
	}
	return json.RawMessage(`[]`)
}

// launch reads a started segment's stream in the background.
func (s *Agent) launch(ctx context.Context, seg segment, body io.ReadCloser, cancel context.CancelFunc) {
	go s.readRun(context.WithoutCancel(ctx), seg, body, cancel)
}
