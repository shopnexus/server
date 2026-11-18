package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
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

// Events is UC-MOD-08 step 3 as the member watches it: the agent's event
// stream of a turn, replayed after lastEventID (a seq) and then followed
// live. The body is text/event-stream as the agent writes it; the caller
// copies it and closes it, which ends the read upstream and not the run.
// Only the wait for the stream to open is bounded; a stream runs as long
// as the turn and the member's tab.
func (s *Agent) Events(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, lastEventID string) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(ctx)
	target := strings.TrimSuffix(s.baseURL, "/") + "/turns/" + turn.String() + "/events"
	req, err := s.newRequest(ctx, caller, http.MethodGet, target, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	opened := time.AfterFunc(readTimeout, cancel)
	res, err := s.http.Do(req)
	if !opened.Stop() {
		cancel()
		if res != nil {
			_ = res.Body.Close()
		}
		return nil, domain.ErrUnavailable.WithCause(errors.New("the agent's event stream did not open in time"))
	}
	if err != nil {
		cancel()
		return nil, domain.ErrUnavailable.WithCause(err)
	}
	if res.StatusCode != http.StatusOK {
		defer cancel()
		defer func() { _ = res.Body.Close() }()
		return nil, refusalOf(res, http.MethodGet, "/turns/{id}/events", domain.ErrTurnNotFound, nil)
	}
	return stream{ReadCloser: res.Body, cancel: cancel}, nil
}

// stream is an open event stream; Close ends the request.
type stream struct {
	io.ReadCloser

	cancel context.CancelFunc
}

func (s stream) Close() error {
	s.cancel()
	return s.ReadCloser.Close() //nolint:wrapcheck // Close of the body, as an io.ReadCloser passes it on.
}

// Turn reads a turn as the agent keeps it: its status, its answer, its
// sources, its steering and, while it waits, its questions.
func (s *Agent) Turn(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Turn, error) {
	var got Turn
	if err := s.send(ctx, caller, request{
		method: http.MethodGet, path: "/turns/" + id.String(), out: &got, timeout: readTimeout,
		late: domain.ErrUnavailable, notFound: domain.ErrTurnNotFound,
	}); err != nil {
		return Turn{}, err
	}
	return got.lists(), nil
}

// Control is UC-MOD-08's run controls: pause takes effect before the next
// model call, so no tool is cut halfway; stop ends the call in flight and
// pauses the run; continue resumes a paused, failed or timed out turn that
// is its conversation's latest; end cancels a paused or waiting one. After
// continue the console opens the stream again from its last seq.
func (s *Agent) Control(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, c Control) (TurnState, error) {
	var got TurnState
	if err := s.turnCall(ctx, caller, turn, string(c), nil, &got, nil); err != nil {
		return TurnState{}, err
	}
	return TurnState{TurnID: turn, Status: strings.ToLower(got.Status)}, nil
}

// Steer is UC-MOD-08's steering: text the run reads before its next model
// call, or that continues a paused run.
func (s *Agent) Steer(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, text string) (Steering, error) {
	var got Steering
	if err := s.turnCall(ctx, caller, turn, "steer", map[string]string{"text": text}, &got, nil); err != nil {
		return Steering{}, err
	}
	got.TurnID = turn
	return got, nil
}

// Answer gives a turn that waits on the agent's clarifying questions the
// member's answers, and the run goes on; answers that do not fit the
// questions are agent_answers_invalid.
func (s *Agent) Answer(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, answers Answers) (TurnState, error) {
	if err := s.turnCall(ctx, caller, turn, "input", map[string]Answers{"answers": answers}, &struct{}{},
		domain.ErrAnswersInvalid); err != nil {
		return TurnState{}, err
	}
	return TurnState{TurnID: turn}, nil
}

func (s *Agent) turnCall(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, action string, in, out any,
	badRequest *apperr.Error,
) error {
	if in == nil {
		in = struct{}{}
	}
	return s.send(ctx, caller, request{
		method: http.MethodPost, path: "/turns/" + turn.String() + "/" + action, in: in, out: out, timeout: readTimeout,
		late: domain.ErrUnavailable, notFound: domain.ErrTurnNotFound, badRequest: badRequest,
	})
}

// ToolCall reads one tool call of a turn in full: its input, its output
// and its timing, for the console's tool panel.
func (s *Agent) ToolCall(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ToolCall, error) {
	var got ToolCall
	if err := s.send(ctx, caller, request{
		method: http.MethodGet, path: "/tool-calls/" + id.String(), out: &got, timeout: readTimeout,
		late: domain.ErrUnavailable, notFound: domain.ErrToolCallNotFound,
	}); err != nil {
		return ToolCall{}, err
	}
	return got, nil
}

// Document reads a document a turn wrote, with every version.
func (s *Agent) Document(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Document, error) {
	var got Document
	if err := s.send(ctx, caller, request{
		method: http.MethodGet, path: "/documents/" + id.String(), out: &got, timeout: readTimeout,
		late: domain.ErrUnavailable, notFound: domain.ErrDocumentNotFound,
	}); err != nil {
		return Document{}, err
	}
	if got.Versions == nil {
		got.Versions = []DocumentVersion{}
	}
	for i := range got.Versions {
		if got.Versions[i].Sections == nil {
			got.Versions[i].Sections = []json.RawMessage{}
		}
	}
	return got, nil
}

// Skill reads one skill.
func (s *Agent) Skill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Skill, error) {
	return s.skillCall(ctx, caller, http.MethodGet, "/skills/"+id.String(), nil)
}

// Skills lists every skill; each is shared with every member.
func (s *Agent) Skills(ctx context.Context, caller authn.ConsoleMember) ([]Skill, error) {
	var got []Skill
	if err := s.send(ctx, caller, request{
		method: http.MethodGet, path: "/skills", out: &got, timeout: readTimeout,
		late: domain.ErrUnavailable, notFound: domain.ErrSkillNotFound,
	}); err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(got))
	for i := range got {
		sk := &got[i]
		out = append(out, sk.lists())
	}
	return out, nil
}

// CreateSkill saves a skill made by caller.
func (s *Agent) CreateSkill(ctx context.Context, caller authn.ConsoleMember, in SkillInput) (Skill, error) {
	return s.skillCall(ctx, caller, http.MethodPost, "/skills", in)
}

// UpdateSkill changes a skill; any member may.
func (s *Agent) UpdateSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, in SkillPatch) (Skill, error) {
	return s.skillCall(ctx, caller, http.MethodPatch, "/skills/"+id.String(), in)
}

// DeleteSkill deletes a skill.
func (s *Agent) DeleteSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error {
	return s.send(ctx, caller, request{
		method: http.MethodDelete, path: "/skills/" + id.String(), timeout: readTimeout,
		late: domain.ErrUnavailable, notFound: domain.ErrSkillNotFound, badRequest: domain.ErrSkillNotFound,
	})
}

func (s *Agent) skillCall(ctx context.Context, caller authn.ConsoleMember, method, path string, in any) (Skill, error) {
	var got Skill
	if err := s.send(ctx, caller, request{
		method: method, path: path, in: in, out: &got, timeout: readTimeout,
		late: domain.ErrUnavailable, notFound: domain.ErrSkillNotFound, badRequest: domain.ErrSkillInvalid,
	}); err != nil {
		return Skill{}, err
	}
	return got.lists(), nil
}
