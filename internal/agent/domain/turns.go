package domain

// A turn's status, as agent_turns stores it and the console reads it.
// Only running holds the conversation; the others take a new message, and
// the conversation's last turn may resume.
const (
	TurnRunning   = "running"
	TurnPaused    = "paused"
	TurnStopped   = "stopped"
	TurnSucceeded = "succeeded"
	TurnFailed    = "failed"
	TurnTimedOut  = "timed_out"
)

// Why the runtime ended a run without an error (its TurnResult.Stop).
const (
	StopDone     = "done"
	StopDeadline = "deadline"
	StopPaused   = "paused"
	StopStopped  = "stopped"
	StopBudget   = "budget" // the model gateway refused: the agent's budget is spent
)

// StatusOf is the status a run ends a turn in, from the runtime's stop
// reason and error.
func StatusOf(stop, err string) string {
	switch {
	case err != "", stop == StopBudget:
		return TurnFailed
	case stop == StopPaused:
		return TurnPaused
	case stop == StopStopped:
		return TurnStopped
	case stop == StopDeadline:
		return TurnTimedOut
	default:
		return TurnSucceeded
	}
}

// Message is one message of a conversation's history, the agent runtime's
// agent.Message as it travels in JSON. The server stores it whole and reads
// only Role.
type Message struct {
	Role       string     `json:"role"`
	Context    string     `json:"context,omitempty"`
	Content    string     `json:"content,omitempty"`
	Reasoning  string     `json:"reasoning,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	IsError    bool       `json:"is_error,omitempty"`
}

// ToolCall is a command the model ran in its sandbox.
type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

// The approval of a change an agent asked for.
const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
	ApprovalFailed   = "failed"
)
