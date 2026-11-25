package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Ask is UC-MOD-08 steps 2 and 3. The server checks the agent the member
// picked, the budget, the rate limit and the question, opens the turn
// running and starts its run on the agent service, which streams the run
// back; the console follows it through Events. The agent reads through
// the MCP server's read-only tools and holds no write tool, so a question
// changes no business data. A key the member used answers its turn and
// starts no run.
func (s *Agent) Ask(ctx context.Context, caller authn.ConsoleMember, conversation uuid.UUID, in NewTurn) (TurnStarted, error) {
	if in.Key != nil {
		if started, ok, err := s.turnOfKey(ctx, caller, *in.Key); err != nil || ok {
			return started, err
		}
	}
	id := domain.ConsoleAssistant
	if in.AgentID != nil {
		id = *in.AgentID
	}
	a, err := s.d.Settings.Agent(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && a.Kind != domain.AgentConsole) {
		return TurnStarted{}, domain.ErrAgentNotFound
	}
	if err != nil {
		return TurnStarted{}, fmt.Errorf("read the turn's agent: %w", err)
	}
	if !a.Enabled {
		return TurnStarted{}, domain.ErrAgentDisabled
	}
	set, err := s.d.Settings.BudgetSettings(ctx)
	if err != nil {
		return TurnStarted{}, fmt.Errorf("read the budget settings: %w", err)
	}
	// E1 the month's cap on AI spend is reached and stops the agents.
	if s.capReached(ctx, set) {
		return TurnStarted{}, domain.ErrBudget
	}
	s.ensureMemberBudget(ctx, set, caller)
	c, err := s.d.Runs.Conversation(ctx, conversation)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !visible(c, caller)) {
		return TurnStarted{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return TurnStarted{}, fmt.Errorf("read the conversation: %w", err)
	}
	var skill *runSkill
	if in.SkillID != nil {
		sk, skillErr := s.d.Runs.Skill(ctx, *in.SkillID)
		if errors.Is(skillErr, repository.ErrNotFound) {
			return TurnStarted{}, domain.ErrSkillInvalid
		}
		if skillErr != nil {
			return TurnStarted{}, fmt.Errorf("read the turn's skill: %w", skillErr)
		}
		skill = &runSkill{ID: sk.ID, Name: sk.Name, Prompt: sk.Prompt}
	}
	now := s.d.Now().UTC().Truncate(time.Microsecond)
	if !s.limiter.allow(caller.MemberID, now) {
		return TurnStarted{}, domain.ErrRateLimited
	}
	text, err := domain.CleanInput(in.Text)
	if err != nil {
		return TurnStarted{}, err
	}
	attachments, err := domain.CheckAttachments(in.Attachments)
	if err != nil {
		return TurnStarted{}, err
	}
	limits, err := a.Limits.Check(domain.AgentConsole)
	if err != nil {
		return TurnStarted{}, fmt.Errorf("the turn's agent's limits: %w", err)
	}
	deadline := time.Duration(limits.DeadlineS) * time.Second
	// 2. The turn opens running under the conversation's lock, one
	// unfinished turn at a time.
	t := repository.Turn{
		ID: uuid.NewV7(), ConversationID: conversation, MemberID: caller.MemberID, Question: text, AgentID: a.ID.String(),
		AgentName: a.Name, ModelAlias: a.Alias, StartedAt: now, RunStartedAt: now, DeadlineMS: deadline.Milliseconds(),
		SubjectKind: c.SubjectKind, SubjectID: c.SubjectID, Attachments: attachments, RequestKey: in.Key,
	}
	switch err = s.d.Runs.OpenTurn(ctx, t, caller.MemberID); {
	case errors.Is(err, repository.ErrKeyUsed):
		// A racing retry of the key lost to the first; answer the first.
		if started, ok, keyErr := s.turnOfKey(ctx, caller, *in.Key); keyErr != nil || ok {
			return started, keyErr
		}
		return TurnStarted{}, apperr.ErrTryAgain
	case errors.Is(err, repository.ErrNotFound):
		return TurnStarted{}, domain.ErrConversationNotFound
	case errors.Is(err, repository.ErrBusy):
		return TurnStarted{}, domain.ErrTurnInProgress
	case err != nil:
		return TurnStarted{}, fmt.Errorf("open the turn: %w", err)
	}
	// 3. The run goes out with the agent's configuration, so the server
	// stays its only source, the prior turns it replays and the records the
	// conversation mentions. · ADR-0003
	// A1 past the agent's deadline the run ends TIMED_OUT with what it
	// found, and the member may continue it.
	history, err := s.d.Runs.History(ctx, conversation, historyTurns)
	var docs []repository.DocumentRef
	if err == nil {
		docs, err = s.d.Runs.Documents(ctx, conversation)
	}
	var mentions []domain.Attachment
	if err == nil {
		mentions, err = s.d.Runs.Mentions(ctx, t.ID, domain.MaxMentions)
	}
	var stream io.ReadCloser
	var cancel context.CancelFunc
	if err == nil {
		req := newRequest(t, caller, "new", docs, mentions)
		req.AgentConfig = configOf(a)
		req.Skill, req.History = skill, historyOf(history)
		stream, cancel, err = s.startRun(ctx, req, deadline)
	}
	if err != nil {
		if delErr := s.d.Runs.DeleteTurn(context.WithoutCancel(ctx), t.ID); delErr != nil {
			s.d.Log.WarnContext(ctx, "agent: delete a turn whose run did not start", "turn_id", t.ID, "err", delErr)
		}
		return TurnStarted{}, err
	}
	// The conversation takes the question's time and title only once the
	// run started, so a failed start leaves it as it was.
	if err = s.d.Runs.TouchConversation(ctx, conversation, text, now); err != nil {
		s.d.Log.WarnContext(ctx, "agent: touch a conversation whose run started", "conversation_id", conversation, "err", err)
	}
	s.launch(ctx, segment{turn: t.ID, conversation: conversation, member: caller.MemberID, start: now}, stream, cancel)
	return TurnStarted{ConversationID: conversation, TurnID: t.ID}, nil
}

// turnOfKey reads the turn caller opened with key, when there is one.
func (s *Agent) turnOfKey(ctx context.Context, caller authn.ConsoleMember, key uuid.UUID) (TurnStarted, bool, error) {
	t, err := s.d.Runs.TurnByKey(ctx, caller.MemberID, key)
	if errors.Is(err, repository.ErrNotFound) {
		return TurnStarted{}, false, nil
	}
	if err != nil {
		return TurnStarted{}, false, fmt.Errorf("read the key's turn: %w", err)
	}
	return TurnStarted{ConversationID: t.ConversationID, TurnID: t.ID}, true, nil
}
