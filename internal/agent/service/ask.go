package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Ask is UC-MOD-08 steps 2 and 3. The server checks the agent the member
// picked, adds its configuration and forwards: the agent stores the turn
// RUNNING, answers with its id at once and runs it, and the console
// follows the run through Events. The agent reads through the MCP server's
// read-only tools and holds no write tool, so a question changes no
// business data.
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
	// 2. The question goes out with the member's id and role, which the
	// agent writes on the turn, and the agent's configuration, so the
	// server stays its only source. · ADR-0003
	body := turnRequest{
		Text: in.Text, Attachments: in.Attachments, SkillID: in.SkillID,
		AgentConfig: AgentConfig{
			ID: a.ID, Name: a.Name, Alias: a.Alias, Instructions: a.Instructions,
			InstructionsVersion: a.InstructionsVersion, Tools: a.Tools, Limits: a.Limits,
		},
	}
	var got TurnStarted
	// 3. The agent reads, shows its sources and answers, step by step on
	// the turn's events. · BR-MOD-03
	// A1 past the agent's deadline the run ends TIMED_OUT with what it
	// found, and the member may continue it.
	// E1 over the budget the agent answers agent_budget, and the console
	// works on without it.
	if err = s.call(ctx, caller, http.MethodPost, "/conversations/"+conversation.String()+"/turns", nil,
		body, &got); err != nil {
		return TurnStarted{}, err
	}
	got.ConversationID = conversation
	return got, nil
}

// turnRequest is the body of the agent's POST /conversations/{id}/turns.
type turnRequest struct {
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments,omitempty"`
	SkillID     *uuid.UUID   `json:"skill_id,omitempty"`
	AgentConfig AgentConfig  `json:"agent_config"`
}
