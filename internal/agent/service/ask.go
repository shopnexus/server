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
	"github.com/shopnexus/server/internal/platform/authn"
)

// Ask is UC-MOD-08 steps 2 and 3. The server checks the agent the member
// picked, the budget, the rate limit and the question, opens the turn
// running and starts its run on the agent service, which streams the run
// back; the console follows it through Events. The agent reads through
// the MCP server's read-only tools and holds no write tool, so a question
// changes no business data.
func (s *Agent) Ask(ctx context.Context, caller authn.ConsoleMember, conversation uuid.UUID, in NewTurn) (TurnStarted, error) {
	id := domain.ConsoleAssistant
	if in.AgentID != nil {
		id = *in.AgentID
	}
	a, err := s.settings.Agent(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && a.Kind != domain.AgentConsole) {
		return TurnStarted{}, domain.ErrAgentNotFound
	}
	if err != nil {
		return TurnStarted{}, fmt.Errorf("read the turn's agent: %w", err)
	}
	if !a.Enabled {
		return TurnStarted{}, domain.ErrAgentDisabled
	}
	set, err := s.settings.BudgetSettings(ctx)
	if err != nil {
		return TurnStarted{}, fmt.Errorf("read the budget settings: %w", err)
	}
	// E1 the month's cap on AI spend is reached and stops the agents.
	if s.capReached(ctx, set) {
		return TurnStarted{}, domain.ErrBudget
	}
	s.ensureMemberBudget(ctx, set, caller)
	c, err := s.runs.Conversation(ctx, conversation)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !visible(c, caller)) {
		return TurnStarted{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return TurnStarted{}, fmt.Errorf("read the conversation: %w", err)
	}
	if c.Status != "active" {
		return TurnStarted{}, domain.ErrConversationClosed
	}
	var skill *runSkill
	if in.SkillID != nil {
		sk, skillErr := s.runs.Skill(ctx, *in.SkillID)
		if errors.Is(skillErr, repository.ErrNotFound) {
			return TurnStarted{}, domain.ErrSkillInvalid
		}
		if skillErr != nil {
			return TurnStarted{}, fmt.Errorf("read the turn's skill: %w", skillErr)
		}
		skill = &runSkill{ID: sk.ID, Name: sk.Name, Prompt: sk.Prompt}
	}
	now := s.now().UTC().Truncate(time.Microsecond)
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
		SubjectKind: c.SubjectKind, SubjectID: c.SubjectID, Attachments: attachments,
	}
	switch err = s.runs.OpenTurn(ctx, t, caller.MemberID); {
	case errors.Is(err, repository.ErrNotFound):
		return TurnStarted{}, domain.ErrConversationNotFound
	case errors.Is(err, repository.ErrClosed):
		return TurnStarted{}, domain.ErrConversationClosed
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
	history, err := s.runs.History(ctx, conversation, historyTurns)
	var docs []repository.DocumentRef
	if err == nil {
		docs, err = s.runs.Documents(ctx, conversation)
	}
	var mentions []domain.Attachment
	if err == nil {
		mentions, err = s.runs.Mentions(ctx, t.ID, domain.MaxMentions)
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
		if delErr := s.runs.DeleteTurn(context.WithoutCancel(ctx), t.ID); delErr != nil {
			s.log.WarnContext(ctx, "agent: delete a turn whose run did not start", "turn_id", t.ID, "err", delErr)
		}
		return TurnStarted{}, err
	}
	s.launch(ctx, segment{turn: t.ID, conversation: conversation, member: caller.MemberID, start: now}, stream, cancel)
	return TurnStarted{ConversationID: conversation, TurnID: t.ID}, nil
}
