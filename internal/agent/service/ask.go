package service

import (
	"context"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Ask is UC-MOD-08 steps 2 and 3. The server only forwards: the agent
// records the turn with the member who asked, reads through the MCP
// server's read-only tools and answers with the sources it read. It holds
// no write tool, so a question changes no business data.
func (s *Agent) Ask(ctx context.Context, caller authn.ConsoleMember, conversation uuid.UUID, text string) (Answer, error) {
	// 2. The question goes out with the member's id and role, which the
	// agent writes on the turn. · ADR-0003
	var got Answer
	// 3. The agent reads, shows its sources and answers. · BR-MOD-03
	// A1 past the deadline the turn stops with agent_timeout, and the member
	// asks again; the background run comes with AgentRuns.
	// E1 over the day's budget the agent answers agent_budget, and the
	// console works on without it.
	if err := s.call(ctx, caller, http.MethodPost, "/conversations/"+conversation.String()+"/turns", nil,
		map[string]string{"text": text}, &got, turnTimeout, domain.ErrTimeout); err != nil {
		return Answer{}, err
	}
	got.Sources = sourcesOf(got.Sources)
	return got, nil
}
