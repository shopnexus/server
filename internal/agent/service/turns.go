package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Control is what a member does to a run: pause it before its next model
// call, stop it now, continue a paused or broken run, or end a paused one.
type Control string

const (
	ControlPause    Control = "pause"
	ControlStop     Control = "stop"
	ControlContinue Control = "continue"
	ControlEnd      Control = "end"
)

// rateLimitedDetail is the guardrail event's detail for an update the rate
// limit refused.
const rateLimitedDetail = "More than 30 messages a minute; the update was not sent."

// visibleTurn reads a turn of a conversation caller may open.
func (s *Agent) visibleTurn(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (repository.Turn, error) {
	t, err := s.runs.Turn(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && t.SubjectKind == "" && t.Owner != caller.MemberID) {
		return repository.Turn{}, domain.ErrTurnNotFound
	}
	if err != nil {
		return repository.Turn{}, fmt.Errorf("read the turn: %w", err)
	}
	return t, nil
}

// Turn reads a turn: its status, its answer, its sources, its steering and,
// while it waits, its questions.
func (s *Agent) Turn(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Turn, error) {
	t, err := s.visibleTurn(ctx, caller, id)
	if err != nil {
		return Turn{}, err
	}
	steers, err := s.runs.Steers(ctx, []uuid.UUID{id})
	if err != nil {
		return Turn{}, fmt.Errorf("read the turn's steers: %w", err)
	}
	return turnView(t, steers[id]), nil
}

// Control is UC-MOD-08's run controls: pause takes effect before the next
// model call, so no tool is cut halfway; stop ends the call in flight and
// pauses the run; continue resumes a paused, failed or timed out turn that
// is its conversation's latest; end cancels a paused or waiting one. Pause
// and stop wait in the turn's inputs, which the agent polls. After
// continue the console opens the stream again from its last seq.
func (s *Agent) Control(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, c Control) (TurnState, error) {
	t, err := s.visibleTurn(ctx, caller, turn)
	if err != nil {
		return TurnState{}, err
	}
	switch c {
	case ControlPause, ControlStop:
		if t.Status != domain.TurnRunning {
			return TurnState{}, domain.ErrTurnState
		}
		err = s.runs.AddInput(ctx, repository.Input{
			ID: uuid.NewV7(), TurnID: turn, Kind: string(c), CreatedAt: s.now(), MemberID: caller.MemberID,
		})
		if errors.Is(err, repository.ErrState) {
			return TurnState{}, domain.ErrTurnState
		}
		if err != nil {
			return TurnState{}, fmt.Errorf("send %s: %w", c, err)
		}
		return TurnState{TurnID: turn}, nil
	case ControlContinue:
		ok := t.Status == domain.TurnPaused || domain.Continuable(t.Status, t.ErrorCode, t.Wrote, t.Latest)
		if !ok {
			return TurnState{}, domain.ErrTurnState
		}
		if err = s.resume(ctx, caller, t, "continue", nil, ""); err != nil {
			return TurnState{}, err
		}
		return TurnState{TurnID: turn}, nil
	case ControlEnd:
		if t.Status != domain.TurnPaused && t.Status != domain.TurnAwaitingInput {
			return TurnState{}, domain.ErrTurnState
		}
		sources := sourcesOf(t)
		body, encErr := json.Marshal(map[string]any{
			"status": "CANCELLED", "outcome": domain.Outcome(domain.TurnCancelled, ""), "continuable": false, "sources": sources,
		})
		if encErr != nil {
			return TurnState{}, fmt.Errorf("encode turn.finished: %w", encErr)
		}
		err = s.runs.CancelTurn(ctx, turn, t.Status, sources, body, s.now())
		if errors.Is(err, repository.ErrState) {
			return TurnState{}, domain.ErrTurnState
		}
		if err != nil {
			return TurnState{}, fmt.Errorf("end the turn: %w", err)
		}
		return TurnState{TurnID: turn, Status: domain.TurnCancelled}, nil
	}
	return TurnState{}, domain.ErrTurnState
}

// Steer is UC-MOD-08's steering: text a running run reads before its next
// model call, or that continues a paused run. A key the member used
// answers its steer and sends nothing.
func (s *Agent) Steer(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, text string, key *uuid.UUID) (Steering, error) {
	t, err := s.visibleTurn(ctx, caller, turn)
	if err != nil {
		return Steering{}, err
	}
	if key != nil {
		if st, ok, keyErr := s.steerOfKey(ctx, caller, *key); keyErr != nil || ok {
			return st, keyErr
		}
	}
	now := s.now()
	if !s.limiter.allow(caller.MemberID, now) {
		if t.Status == domain.TurnRunning || t.Status == domain.TurnPaused {
			body, _ := json.Marshal(map[string]string{"rule": "rate_limited", "detail": rateLimitedDetail})
			if _, evErr := s.runs.AppendEvent(ctx, turn, "guardrail", body, now); evErr != nil {
				s.log.WarnContext(ctx, "agent: write a guardrail event", "turn_id", turn, "err", evErr)
			}
		}
		return Steering{}, domain.ErrRateLimited
	}
	clean, err := domain.CleanInput(text)
	if err != nil {
		return Steering{}, err
	}
	in := repository.Input{
		ID: uuid.NewV7(), TurnID: turn, Kind: "steer", Text: clean, CreatedAt: now, MemberID: caller.MemberID, RequestKey: key,
	}
	switch t.Status {
	case domain.TurnRunning:
		err = s.runs.AddInput(ctx, in)
	case domain.TurnPaused:
		in.Resumed = true
		err = s.resume(ctx, caller, t, "steer", &in, "")
	default:
		return Steering{}, domain.ErrTurnState
	}
	if err != nil && key != nil {
		// A racing retry of the key lost to the first, which may have moved
		// the turn on; answer the first.
		if st, ok, keyErr := s.steerOfKey(ctx, caller, *key); keyErr == nil && ok {
			return st, nil
		}
	}
	switch {
	case errors.Is(err, repository.ErrState):
		return Steering{}, domain.ErrTurnState
	case errors.Is(err, repository.ErrKeyUsed):
		return Steering{}, apperr.ErrTryAgain
	case err != nil && t.Status == domain.TurnRunning:
		return Steering{}, fmt.Errorf("send the steer: %w", err)
	case err != nil:
		return Steering{}, err
	}
	return Steering{SteerID: in.ID, TurnID: turn, Resumed: in.Resumed}, nil
}

// steerOfKey reads the steer caller sent with key, when there is one.
func (s *Agent) steerOfKey(ctx context.Context, caller authn.ConsoleMember, key uuid.UUID) (Steering, bool, error) {
	in, err := s.runs.InputByKey(ctx, caller.MemberID, key)
	if errors.Is(err, repository.ErrNotFound) {
		return Steering{}, false, nil
	}
	if err != nil {
		return Steering{}, false, fmt.Errorf("read the key's steer: %w", err)
	}
	return Steering{SteerID: in.ID, TurnID: in.TurnID, Resumed: in.Resumed}, true, nil
}

// Answer gives a turn that waits on the agent's clarifying questions the
// member's answers, and the run goes on; answers that do not fit the
// questions are agent_answers_invalid.
func (s *Agent) Answer(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, answers Answers) (TurnState, error) {
	t, err := s.visibleTurn(ctx, caller, turn)
	if err != nil {
		return TurnState{}, err
	}
	if t.Status != domain.TurnAwaitingInput {
		return TurnState{}, domain.ErrTurnState
	}
	var questions []domain.Question
	if ask := nullable(stateOf(t).PendingAsk); ask != nil {
		if err = json.Unmarshal(ask, &questions); err != nil {
			return TurnState{}, fmt.Errorf("read the turn's questions: %w", err)
		}
	}
	text, err := domain.AnswerText(questions, answers)
	if err != nil {
		return TurnState{}, err
	}
	raw, err := json.Marshal(answers)
	if err != nil {
		return TurnState{}, fmt.Errorf("encode the answers: %w", err)
	}
	now := s.now()
	in := repository.Input{
		ID: uuid.NewV7(), TurnID: turn, Kind: "answers", Text: text, Answers: raw, ReadAt: &now, CreatedAt: now,
		MemberID: caller.MemberID,
	}
	if err = s.resume(ctx, caller, t, "input", &in, text); err != nil {
		return TurnState{}, err
	}
	return TurnState{TurnID: turn}, nil
}

// resume runs the next segment of t, which the member continued (by), as
// caller: the turn goes running under its lock with in stored, the run
// starts from its last checkpoint with the unread steers and answers, and
// turn.resumed is written once the agent took it. A refusal puts the turn
// back as it was.
func (s *Agent) resume(ctx context.Context, caller authn.ConsoleMember, t repository.Turn, by string, in *repository.Input,
	answers string,
) error {
	now := s.now().UTC().Truncate(time.Microsecond)
	res, err := s.runs.ResumeTurn(ctx, repository.Resume{
		TurnID: t.ID, From: t.Status, At: now, Input: in,
		ClearAnswer: t.Status == domain.TurnFailed || t.Status == domain.TurnTimedOut,
	})
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return domain.ErrTurnNotFound
	case errors.Is(err, repository.ErrState):
		return domain.ErrTurnState
	case err != nil:
		return fmt.Errorf("resume the turn: %w", err)
	}
	req := newRequest(res.Turn, caller, "resume", res.Documents, res.Mentions)
	req.State, req.Answers, req.ControlAfter = res.Turn.State, answers, res.ControlAfter
	steers := make([]runSteer, 0, len(res.Steers))
	for i := range res.Steers {
		steers = append(steers, runSteer{SteerID: res.Steers[i].ID, Text: res.Steers[i].Text})
	}
	req.Steers = steers
	if id, parseErr := uuid.Parse(t.AgentID); parseErr == nil {
		if a, agentErr := s.settings.Agent(ctx, id); agentErr == nil {
			req.AgentConfig = configOf(a)
		}
	}
	stream, cancel, err := s.startRun(ctx, req, time.Duration(t.DeadlineMS)*time.Millisecond)
	if err != nil {
		var input *uuid.UUID
		if in != nil {
			input = &in.ID
		}
		if restoreErr := s.runs.RestoreTurn(context.WithoutCancel(ctx), t, input); restoreErr != nil {
			s.log.WarnContext(ctx, "agent: put back a turn whose run did not start", "turn_id", t.ID, "err", restoreErr)
		}
		return err
	}
	body, _ := json.Marshal(map[string]string{"by": by})
	if _, err = s.runs.AppendEvent(ctx, t.ID, "turn.resumed", body, now); err != nil {
		s.log.WarnContext(ctx, "agent: write turn.resumed", "turn_id", t.ID, "err", err)
	}
	s.launch(ctx, segment{turn: t.ID, conversation: t.ConversationID, member: caller.MemberID, start: res.Turn.RunStartedAt},
		stream, cancel)
	return nil
}

// configOf is the agent_config a run of a goes out with.
func configOf(a repository.Agent) *AgentConfig {
	return &AgentConfig{
		ID: a.ID, Name: a.Name, Alias: a.Alias, Instructions: a.Instructions, InstructionsVersion: a.InstructionsVersion,
		Tools: a.Tools, Limits: a.Limits,
	}
}

// TurnControl is what a running segment polls: the pauses, stops and
// steers after a seq, and whether the turn still runs.
func (s *Agent) TurnControl(ctx context.Context, turn uuid.UUID, after int64) (TurnControl, error) {
	list, running, err := s.runs.Control(ctx, turn, after, controlPage)
	if err != nil {
		return TurnControl{}, fmt.Errorf("read the turn's control: %w", err)
	}
	out := TurnControl{Items: make([]ControlItem, 0, len(list)), Running: running}
	for i := range list {
		in := &list[i]
		item := ControlItem{Seq: in.Seq, Kind: in.Kind}
		if in.Kind == "steer" {
			item.SteerID, item.Text = &in.ID, in.Text
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// controlPage is the most inputs one poll returns.
const controlPage = 100

// ToolCall reads one tool call of a turn in full: its input, its output
// and its timing, for the console's tool panel.
func (s *Agent) ToolCall(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ToolCall, error) {
	tc, err := s.runs.ToolCall(ctx, id, caller.MemberID)
	if errors.Is(err, repository.ErrNotFound) {
		return ToolCall{}, domain.ErrToolCallNotFound
	}
	if err != nil {
		return ToolCall{}, fmt.Errorf("read the tool call: %w", err)
	}
	out := ToolCall{
		ID: tc.ID, TurnID: tc.TurnID, Tool: tc.Tool, Status: tc.Status, Input: tc.Input, OutputSpilled: tc.OutputSpilled,
		Attempts: tc.Attempts, StartedAt: tc.CreatedAt, FinishedAt: tc.FinishedAt, AttemptErrors: []AttemptError{},
	}
	if tc.LatencyMS != nil {
		out.DurationMS = new(int(*tc.LatencyMS))
	}
	if tc.Output != nil {
		if json.Valid([]byte(*tc.Output)) {
			out.Output = json.RawMessage(*tc.Output)
		} else {
			out.Output, _ = json.Marshal(*tc.Output)
		}
	}
	if nullable(tc.Timing) != nil {
		out.Timing = &Timing{}
		_ = json.Unmarshal(tc.Timing, out.Timing)
	}
	if nullable(tc.AttemptErrors) != nil {
		_ = json.Unmarshal(tc.AttemptErrors, &out.AttemptErrors)
	}
	return out, nil
}

// Document reads a document a turn wrote, with every version.
func (s *Agent) Document(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Document, error) {
	d, err := s.runs.Document(ctx, id, caller.MemberID)
	if errors.Is(err, repository.ErrNotFound) {
		return Document{}, domain.ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("read the document: %w", err)
	}
	out := Document{
		ID: d.ID, ConversationID: d.ConversationID, TurnID: d.TurnID, Title: d.Title, LatestVersion: d.LatestVersion,
		CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, Versions: make([]DocumentVersion, 0, len(d.Versions)),
	}
	for _, v := range d.Versions {
		out.Versions = append(out.Versions, DocumentVersion{
			Version: v.Version, Title: v.Title, Sections: rawList(v.Sections), TurnID: v.TurnID, CreatedBy: v.CreatedBy,
			CreatedAt: v.CreatedAt,
		})
	}
	return out, nil
}

// rawList is a JSON array's items, an empty list for anything else.
func rawList(raw json.RawMessage) []json.RawMessage {
	var out []json.RawMessage
	if nullable(raw) != nil {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = []json.RawMessage{}
	}
	return out
}
