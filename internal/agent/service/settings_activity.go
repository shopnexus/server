package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
)

// Activity is the AIAgentActivity schema: what the console agents did over
// a range, for the AI settings' capabilities and guardrails.
type Activity struct {
	Range       string          `json:"range"`
	Calls       []CallCount     `json:"calls"`
	Turns       []TurnCount     `json:"turns"`
	Approvals   []ApprovalCount `json:"approvals"`
	RateLimited int             `json:"rate_limited"`
}

// CallCount counts an agent's calls of an operation.
type CallCount struct {
	AgentID      uuid.UUID `json:"agent_id"`
	Operation    string    `json:"operation"`
	Calls        int       `json:"calls"`
	Held         int       `json:"held"`
	Errors       int       `json:"errors"`
	AvgMS        int       `json:"avg_ms"`
	LastCalledAt time.Time `json:"last_called_at"`
}

// TurnCount counts an agent's turns by how they ended.
type TurnCount struct {
	AgentID uuid.UUID `json:"agent_id"`
	Status  string    `json:"status"`
	Stop    string    `json:"stop,omitempty"`
	Turns   int       `json:"turns"`
}

// ApprovalCount counts an agent's approvals in a status.
type ApprovalCount struct {
	AgentID   uuid.UUID `json:"agent_id"`
	Status    string    `json:"status"`
	Approvals int       `json:"approvals"`
}

// AgentActivity counts the console agents' calls by operation, turns by
// how they ended, approvals by status and the rate limit's refusals over
// a range of days ending today.
func (s *Agent) AgentActivity(ctx context.Context, _ authn.ConsoleMember, rng string) (Activity, error) {
	if _, ok := SpendRanges[rng]; !ok {
		rng = "7d"
	}
	from, _ := rangeDays(s.d.Now(), rangeOf(rng))
	a, err := s.d.Conversations.Activity(ctx, from)
	if err != nil {
		return Activity{}, fmt.Errorf("count the agents' activity: %w", err)
	}
	out := Activity{
		Range: rng, Calls: make([]CallCount, 0, len(a.Calls)), Turns: make([]TurnCount, 0, len(a.Turns)),
		Approvals: make([]ApprovalCount, 0, len(a.Approvals)), RateLimited: a.RateLimited,
	}
	for _, c := range a.Calls {
		out.Calls = append(out.Calls, CallCount{
			AgentID: c.AgentID, Operation: c.Operation, Calls: c.Calls, Held: c.Held, Errors: c.Errors, AvgMS: c.AvgMS,
			LastCalledAt: c.LastAt.UTC(),
		})
	}
	for _, t := range a.Turns {
		out.Turns = append(out.Turns, TurnCount(t))
	}
	for _, x := range a.Approvals {
		out.Approvals = append(out.Approvals, ApprovalCount(x))
	}
	return out, nil
}

// limited notes a refusal of the rate limit, for the guardrails.
func (s *Agent) limited(ctx context.Context, caller authn.ConsoleMember) {
	if err := s.d.Conversations.RecordLimit(context.WithoutCancel(ctx), caller.MemberID, s.d.Now().UTC()); err != nil {
		s.d.Log.WarnContext(ctx, "agent: note a rate limit", "member_id", caller.MemberID, "err", err)
	}
}
