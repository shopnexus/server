package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

// tokenPrefix marks a turn token, so the gate tells it from other bearer
// tokens without a lookup.
const tokenPrefix = "snt_"

// newToken makes a turn token and the hash agent_turns keeps of it.
func newToken() (token string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token)
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// visibleTurn reads a turn of a conversation caller may open.
func (s *Agent) visibleTurn(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (repository.Turn, error) {
	t, err := s.d.Conversations.Turn(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && t.SubjectKind == "" && t.Owner != caller.MemberID) {
		return repository.Turn{}, domain.ErrTurnNotFound
	}
	if err != nil {
		return repository.Turn{}, fmt.Errorf("read the turn: %w", err)
	}
	return t, nil
}

// Turn reads a turn.
func (s *Agent) Turn(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Turn, error) {
	t, err := s.visibleTurn(ctx, caller, id)
	if err != nil {
		return Turn{}, err
	}
	return turnView(t), nil
}

// turnAgent reads the console agent a turn runs, turned on.
func (s *Agent) turnAgent(ctx context.Context, id uuid.UUID) (repository.Agent, error) {
	a, err := s.d.Settings.Agent(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && a.Kind != domain.AgentConsole) {
		return repository.Agent{}, domain.ErrAgentNotFound
	}
	if err != nil {
		return repository.Agent{}, fmt.Errorf("read the turn's agent: %w", err)
	}
	if !a.Enabled {
		return repository.Agent{}, domain.ErrAgentDisabled
	}
	return a, nil
}

// Ask is UC-MOD-08 steps 2 and 3. The server checks the agent, the rate
// limit and the message, opens the turn running with a token that holds
// the agent's capabilities, and starts its run on the agent runtime over
// the conversation's history and the message; the console follows the run
// through Events. A key the member used answers its turn and starts none.
func (s *Agent) Ask(ctx context.Context, caller authn.ConsoleMember, conversation uuid.UUID, in NewTurn) (TurnStarted, error) {
	if in.Key != nil {
		t, err := s.d.Conversations.TurnByKey(ctx, caller.MemberID, *in.Key)
		if err == nil {
			return TurnStarted{ConversationID: t.ConversationID, TurnID: t.ID}, nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return TurnStarted{}, fmt.Errorf("read the key's turn: %w", err)
		}
	}
	id := domain.ConsoleAssistant
	if in.AgentID != nil {
		id = *in.AgentID
	}
	a, err := s.turnAgent(ctx, id)
	if err != nil {
		return TurnStarted{}, err
	}
	c, err := s.visibleConversation(ctx, caller, conversation)
	if err != nil {
		return TurnStarted{}, err
	}
	now := s.d.Now().UTC().Truncate(time.Microsecond)
	if !s.limiter.allow(caller.MemberID, now) {
		s.limited(ctx, caller)
		return TurnStarted{}, domain.ErrRateLimited
	}
	text, err := domain.CleanInput(in.Text)
	if err != nil {
		return TurnStarted{}, err
	}
	route, err := s.routeOf(ctx, a)
	if err != nil {
		return TurnStarted{}, err
	}
	limits, err := a.Limits.Check(domain.AgentConsole)
	if err != nil {
		return TurnStarted{}, fmt.Errorf("the turn's agent's limits: %w", err)
	}
	token, hash := newToken()
	t := repository.Turn{
		ID: uuid.NewV7(), ConversationID: conversation, MemberID: caller.MemberID, AgentID: a.ID, AgentName: a.Name,
		Model: route.model, Capabilities: nonNil(a.Capabilities), Status: domain.TurnRunning, TokenHash: hash,
		RequestKey: in.Key, StartedAt: now, RunStartedAt: now,
		DeadlineAt: now.Add(time.Duration(limits.DeadlineS) * time.Second),
	}
	switch err = s.d.Conversations.OpenTurn(ctx, t, titleOf(text), now); {
	case errors.Is(err, repository.ErrKeyUsed):
		// A racing retry of the key lost to the first; answer the first.
		if first, keyErr := s.d.Conversations.TurnByKey(ctx, caller.MemberID, *in.Key); keyErr == nil {
			return TurnStarted{ConversationID: first.ConversationID, TurnID: first.ID}, nil
		}
		return TurnStarted{}, apperr.ErrTryAgain
	case errors.Is(err, repository.ErrNotFound):
		return TurnStarted{}, domain.ErrConversationNotFound
	case errors.Is(err, repository.ErrBusy):
		return TurnStarted{}, domain.ErrTurnInProgress
	case err != nil:
		return TurnStarted{}, fmt.Errorf("open the turn: %w", err)
	}
	facts, reported, err := s.facts(ctx, caller, c, t.ID, now)
	if err == nil {
		msg, _ := json.Marshal(domain.Message{Role: "user", Context: facts, Content: text})
		err = s.start(ctx, run{
			turn: t, conversation: c, agent: a, route: route, token: token, first: []json.RawMessage{msg},
			reported: reported, by: caller.MemberID, resumed: false,
		})
	}
	if err != nil {
		// The run did not start: the turn goes with it.
		if delErr := s.d.Conversations.DeleteTurn(context.WithoutCancel(ctx), t.ID); delErr != nil {
			s.d.Log.WarnContext(ctx, "agent: delete a turn whose run did not start", "turn_id", t.ID, "err", delErr)
		}
		return TurnStarted{}, err
	}
	return TurnStarted{ConversationID: conversation, TurnID: t.ID}, nil
}

// titleOf is a conversation's title from its first message.
func titleOf(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if r := []rune(line); len(r) > 80 {
		return strings.TrimSpace(string(r[:79])) + "…"
	}
	return line
}

// Resume runs the conversation's last turn again on the history it left,
// with a new token and the agent's capabilities as they are now. The
// decisions on its approvals since then join the history first.
func (s *Agent) Resume(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (TurnState, error) {
	before, err := s.visibleTurn(ctx, caller, id)
	if err != nil {
		return TurnState{}, err
	}
	if before.Status == domain.TurnRunning || !before.Latest {
		return TurnState{}, domain.ErrTurnState
	}
	a, err := s.turnAgent(ctx, before.AgentID)
	if err != nil {
		return TurnState{}, err
	}
	c, err := s.visibleConversation(ctx, caller, before.ConversationID)
	if err != nil {
		return TurnState{}, err
	}
	route, err := s.routeOf(ctx, a)
	if err != nil {
		return TurnState{}, err
	}
	limits, err := a.Limits.Check(domain.AgentConsole)
	if err != nil {
		return TurnState{}, fmt.Errorf("the turn's agent's limits: %w", err)
	}
	now := s.d.Now().UTC().Truncate(time.Microsecond)
	token, hash := newToken()
	res := repository.Resume{
		TurnID: id, TokenHash: hash, RunStartedAt: now, DeadlineAt: now.Add(time.Duration(limits.DeadlineS) * time.Second),
	}
	switch err = s.d.Conversations.ResumeTurn(ctx, res, now); {
	case errors.Is(err, repository.ErrNotFound):
		return TurnState{}, domain.ErrTurnNotFound
	case errors.Is(err, repository.ErrState), errors.Is(err, repository.ErrBusy):
		return TurnState{}, domain.ErrTurnState
	case err != nil:
		return TurnState{}, fmt.Errorf("resume the turn: %w", err)
	}
	t := before
	t.Status, t.TokenHash, t.RunStartedAt, t.DeadlineAt, t.Capabilities = domain.TurnRunning, hash, now, res.DeadlineAt, nonNil(a.Capabilities)
	var first []json.RawMessage
	decided, reported, err := s.decisions(ctx, c.ID)
	if err == nil && decided != "" {
		msg, _ := json.Marshal(domain.Message{Role: "user", Context: decided})
		first = append(first, msg)
	}
	if err == nil {
		err = s.start(ctx, run{
			turn: t, conversation: c, agent: a, route: route, token: token, first: first, reported: reported,
			by: caller.MemberID, resumed: true,
		})
	}
	if err != nil {
		if resetErr := s.d.Conversations.ResetTurn(context.WithoutCancel(ctx), before); resetErr != nil {
			s.d.Log.WarnContext(ctx, "agent: reset a turn whose run did not start", "turn_id", id, "err", resetErr)
		}
		return TurnState{}, err
	}
	return TurnState{TurnID: id, Status: domain.TurnRunning}, nil
}

// facts are the context of a member's message in turn: the time, the
// member and the conversation's record when they are new to it, and the
// approvals decided since the last turn. The model reads them in front of
// the message.
func (s *Agent) facts(ctx context.Context, caller authn.ConsoleMember, c repository.Conversation, turn uuid.UUID,
	now time.Time,
) (string, []uuid.UUID, error) {
	me, err := s.d.Console.ActiveMember(ctx, caller.MemberID)
	if err != nil {
		return "", nil, fmt.Errorf("read the member: %w", err)
	}
	turns, err := s.d.Conversations.Turns(ctx, c.ID)
	if err != nil {
		return "", nil, fmt.Errorf("read the conversation's turns: %w", err)
	}
	var prev *repository.Turn // the turn before this one
	for i := range turns {
		if turns[i].ID != turn {
			prev = &turns[i]
		}
	}
	subject := ""
	if prev == nil && c.SubjectKind != "" && c.SubjectID != nil {
		subject = c.SubjectKind + " " + c.SubjectID.String()
	}
	decided, reported, err := s.decisions(ctx, c.ID)
	if err != nil {
		return "", nil, err
	}
	newWriter := prev == nil || prev.MemberID != caller.MemberID
	return factsText(now, member(me.DisplayName, string(me.Role), me.ID.String()), newWriter, subject, decided), reported, nil
}

// decisions tells the agent what became of the changes it asked for that
// no message told it of yet.
func (s *Agent) decisions(ctx context.Context, conversation uuid.UUID) (string, []uuid.UUID, error) {
	list, err := s.d.Conversations.Unreported(ctx, conversation)
	if err != nil {
		return "", nil, fmt.Errorf("read the decided approvals: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(list))
	for i := range list {
		ids = append(ids, list[i].ID)
	}
	return decisionsText(list), ids, nil
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// gatewayRoute is the model and key a turn calls Bifrost with.
type gatewayRoute struct {
	model, key string
}

// routeOf is the agent's route in Bifrost, through its first caller's
// virtual key.
func (s *Agent) routeOf(ctx context.Context, a repository.Agent) (gatewayRoute, error) {
	if len(a.Callers) == 0 {
		return gatewayRoute{}, domain.ErrUnavailable.WithCause(fmt.Errorf("agent %s has no caller", a.Slug))
	}
	r, err := s.d.Ring.Route(ctx, a.Callers[0].Name)
	if err != nil {
		return gatewayRoute{}, domain.ErrUnavailable.WithCause(err)
	}
	return gatewayRoute{model: r.Model, key: r.Key}, nil
}

// Pause asks the runtime to pause a running turn before its next model
// call; the commands running finish first.
func (s *Agent) Pause(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (TurnState, error) {
	t, err := s.control(ctx, caller, id, "pause", nil)
	if err != nil {
		return TurnState{}, err
	}
	body, _ := json.Marshal(map[string]uuid.UUID{"by": caller.MemberID})
	if _, err = s.d.Conversations.AppendEvent(ctx, id, "turn.pausing", body, s.d.Now().UTC()); err != nil {
		s.d.Log.WarnContext(ctx, "agent: note a pause", "turn_id", id, "err", err)
	}
	return TurnState{TurnID: t.ID, Status: t.Status}, nil
}

// Stop asks the runtime to end a running turn now; the model and the
// commands are cut, and the agent reads that a staff member stopped them.
func (s *Agent) Stop(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (TurnState, error) {
	t, err := s.control(ctx, caller, id, "stop", nil)
	if err != nil {
		return TurnState{}, err
	}
	return TurnState{TurnID: t.ID, Status: t.Status}, nil
}

// Steer hands a running turn a member's message, which the runtime adds
// before the next model call; it joins the history with that step. The
// turn's stream notes it, so the console shows it sent until a step
// carries it.
func (s *Agent) Steer(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, text string) (TurnState, error) {
	text, err := domain.CleanInput(text)
	if err != nil {
		return TurnState{}, err
	}
	if !s.limiter.allow(caller.MemberID, s.d.Now()) {
		s.limited(ctx, caller)
		return TurnState{}, domain.ErrRateLimited
	}
	t, err := s.control(ctx, caller, id, "steer", func(t repository.Turn) (controlRequest, error) {
		other := ""
		if t.MemberID != caller.MemberID {
			me, meErr := s.d.Console.ActiveMember(ctx, caller.MemberID)
			if meErr != nil {
				return controlRequest{}, fmt.Errorf("read the member: %w", meErr)
			}
			other = member(me.DisplayName, string(me.Role), me.ID.String())
		}
		return controlRequest{Content: text, Context: steerText(other)}, nil
	})
	if err != nil {
		return TurnState{}, err
	}
	body, _ := json.Marshal(map[string]any{"text": text, "by": caller.MemberID})
	if _, err = s.d.Conversations.AppendEvent(ctx, id, "steer", body, s.d.Now().UTC()); err != nil {
		s.d.Log.WarnContext(ctx, "agent: note a steer", "turn_id", id, "err", err)
	}
	return TurnState{TurnID: t.ID, Status: t.Status}, nil
}

// control sends a running turn's control to the runtime, with the body
// build makes for the turn.
func (s *Agent) control(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, what string,
	build func(repository.Turn) (controlRequest, error),
) (repository.Turn, error) {
	t, err := s.visibleTurn(ctx, caller, id)
	if err != nil {
		return repository.Turn{}, err
	}
	if t.Status != domain.TurnRunning {
		return repository.Turn{}, domain.ErrTurnState
	}
	var req controlRequest
	if build != nil {
		if req, err = build(t); err != nil {
			return repository.Turn{}, err
		}
	}
	req.TurnID = t.ID.String()
	if err = s.runtimeControl(ctx, t.ConversationID, what, req); err != nil {
		return repository.Turn{}, err
	}
	return t, nil
}
