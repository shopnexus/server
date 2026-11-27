package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/authn"
)

// approvalPage is the most approvals a list returns.
const approvalPage = 100

// TurnOfToken reads the running turn a token belongs to. A token of a run
// that ended, or past its deadline, is invalid.
func (s *Agent) TurnOfToken(ctx context.Context, token string) (authn.Turn, error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return authn.Turn{}, domain.ErrTokenInvalid
	}
	t, err := s.d.Conversations.TurnByToken(ctx, hashToken(token), s.d.Now())
	if errors.Is(err, repository.ErrNotFound) {
		return authn.Turn{}, domain.ErrTokenInvalid
	}
	if err != nil {
		return authn.Turn{}, fmt.Errorf("read the token's turn: %w", err)
	}
	return authn.Turn{ID: t.ID, ConversationID: t.ConversationID, MemberID: t.MemberID, Capabilities: t.Capabilities}, nil
}

// Operation is the operation a route pattern serves, when an agent may be
// given it.
func (s *Agent) Operation(method, pattern string) (domain.Operation, bool) {
	return s.d.Ops.Operation(method, pattern)
}

// Operations lists every operation an agent may be given, for the AI
// settings, or those turn's agent holds, the commands of its CLI.
func (s *Agent) Operations(_ context.Context, _ authn.ConsoleMember, turn *authn.Turn) ([]domain.Operation, error) {
	if turn == nil {
		return s.d.Ops.Operations(nil), nil
	}
	return s.d.Ops.Operations(nonNil(turn.Capabilities)), nil
}

// Hold is UC-MOD-08's change by the agent: the request op would make
// becomes a pending approval on the turn's conversation, which a member
// approves or rejects; the turn's stream shows it.
func (s *Agent) Hold(ctx context.Context, turn authn.Turn, op domain.Operation, req Request) (Approval, error) {
	a := repository.Approval{
		ID: req.ID, ConversationID: turn.ConversationID, TurnID: turn.ID, MemberID: turn.MemberID, Operation: op.Name,
		Method: req.Method, Path: req.Path, Headers: req.Headers, Body: req.Body, Status: domain.ApprovalPending,
		CreatedAt: s.d.Now().UTC().Truncate(time.Microsecond),
	}
	if a.Headers == nil {
		a.Headers = map[string]string{}
	}
	if err := s.d.Conversations.AddApproval(ctx, a); err != nil {
		return Approval{}, fmt.Errorf("store the approval: %w", err)
	}
	v := approvalView(a)
	s.noteApproval(ctx, v)
	return v, nil
}

// noteApproval puts an approval on its turn's stream.
func (s *Agent) noteApproval(ctx context.Context, a Approval) {
	b, _ := json.Marshal(map[string]Approval{"approval": a})
	if _, err := s.d.Conversations.AppendEvent(context.WithoutCancel(ctx), a.TurnID, "approval", b, s.d.Now().UTC()); err != nil {
		s.d.Log.WarnContext(ctx, "agent: note an approval", "approval_id", a.ID, "err", err)
	}
}

// RecordCall records an API call a turn's token made (NFR-PRIV-02: what
// the agent read for whom is kept as the reads are).
func (s *Agent) RecordCall(ctx context.Context, turn authn.Turn, op domain.Operation, method, path string, status int,
	took time.Duration,
) {
	if err := s.d.Conversations.RecordCall(context.WithoutCancel(ctx), repository.Call{
		TurnID: turn.ID, Operation: op.Name, Method: method, Path: path, Status: status, TookMS: took.Milliseconds(),
		At: s.d.Now().UTC(),
	}); err != nil {
		s.d.Log.WarnContext(ctx, "agent: record a turn's call", "turn_id", turn.ID, "err", err)
	}
}

// Approvals lists the approvals of the conversations caller may open, or
// of turn's conversation only.
func (s *Agent) Approvals(ctx context.Context, caller authn.ConsoleMember, turn *authn.Turn, q ApprovalQuery,
) ([]Approval, error) {
	f := repository.ApprovalFilter{Member: caller.MemberID, ConversationID: q.ConversationID, Status: q.Status, Limit: approvalPage}
	if turn != nil {
		if q.ConversationID != nil && *q.ConversationID != turn.ConversationID {
			return []Approval{}, nil
		}
		f.ConversationID = &turn.ConversationID
	}
	rows, err := s.d.Conversations.Approvals(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list the approvals: %w", err)
	}
	out := make([]Approval, 0, len(rows))
	for i := range rows {
		out = append(out, approvalView(rows[i]))
	}
	return out, nil
}

// visibleApproval reads an approval of a conversation caller may open.
func (s *Agent) visibleApproval(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error {
	a, err := s.d.Conversations.Approval(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrApprovalNotFound
	}
	if err != nil {
		return fmt.Errorf("read the approval: %w", err)
	}
	if _, err = s.visibleConversation(ctx, caller, a.ConversationID); errors.Is(err, domain.ErrConversationNotFound) {
		return domain.ErrApprovalNotFound
	}
	return err
}

// Approve is UC-MOD-08's approval of a change: the stored request runs
// through replay as caller, with caller's rights, while the approval is
// locked, so two members never both run it. A 2xx answer approves it, any
// other fails it; the answer is kept for the console and the agent.
func (s *Agent) Approve(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, replay Replay) (Approval, error) {
	return s.decide(ctx, caller, id, func(a repository.Approval) (repository.Decision, error) {
		status, body, err := replay(Request{ID: a.ID, Method: a.Method, Path: a.Path, Headers: a.Headers, Body: a.Body})
		if err != nil {
			return repository.Decision{}, fmt.Errorf("make the approved request: %w", err)
		}
		d := repository.Decision{Status: domain.ApprovalApproved, ResultStatus: &status, ResultBody: asJSON(body)}
		if status < 200 || status > 299 {
			d.Status = domain.ApprovalFailed
		}
		return d, nil
	})
}

// Reject is UC-MOD-08's rejection of a change: nothing runs.
func (s *Agent) Reject(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Approval, error) {
	return s.decide(ctx, caller, id, func(repository.Approval) (repository.Decision, error) {
		return repository.Decision{Status: domain.ApprovalRejected}, nil
	})
}

func (s *Agent) decide(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID,
	decide func(repository.Approval) (repository.Decision, error),
) (Approval, error) {
	if err := s.visibleApproval(ctx, caller, id); err != nil {
		return Approval{}, err
	}
	a, err := s.d.Conversations.Decide(ctx, id, func(a repository.Approval) (repository.Decision, error) {
		d, err := decide(a)
		d.By, d.At = caller.MemberID, s.d.Now().UTC().Truncate(time.Microsecond)
		return d, err
	})
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return Approval{}, domain.ErrApprovalNotFound
	case errors.Is(err, repository.ErrState):
		return Approval{}, domain.ErrApprovalDecided
	case err != nil:
		return Approval{}, fmt.Errorf("decide the approval: %w", err)
	}
	v := approvalView(a)
	s.noteApproval(ctx, v)
	return v, nil
}

// asJSON is body as JSON: itself when it is JSON, else a JSON string of
// it, nil when empty.
func asJSON(body []byte) json.RawMessage {
	if len(body) == 0 {
		return nil
	}
	if json.Valid(body) {
		return body
	}
	s, _ := json.Marshal(string(body))
	return s
}
