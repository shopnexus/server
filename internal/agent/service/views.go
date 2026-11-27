package service

import (
	"encoding/json"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
)

// Conversation is the AgentConversation schema.
type Conversation struct {
	ID          uuid.UUID          `json:"id"`
	SubjectKind domain.SubjectKind `json:"subject_kind,omitempty"`
	SubjectID   *uuid.UUID         `json:"subject_id,omitempty"`
	Title       string             `json:"title,omitempty"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// ConversationDetail is the AgentConversationDetail schema: a
// conversation with its messages and turns.
type ConversationDetail struct {
	Conversation

	Messages []Message `json:"messages"`
	Turns    []Turn    `json:"turns"`
}

// Message is the AgentMessage schema: a domain.Message with its place.
type Message struct {
	domain.Message

	Seq    int        `json:"seq"`
	TurnID *uuid.UUID `json:"turn_id,omitempty"`
}

// Turn is the AgentTurn schema.
type Turn struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	MemberID       uuid.UUID  `json:"member_id"`
	Agent          TurnAgent  `json:"agent"`
	Model          string     `json:"model,omitempty"`
	Status         string     `json:"status"`
	Stop           string     `json:"stop,omitempty"`
	Error          string     `json:"error,omitempty"`
	Usage          Usage      `json:"usage"`
	LastSeq        int64      `json:"last_seq"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// TurnAgent is the agent that ran a turn.
type TurnAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Usage is the AgentUsage schema, a turn's tokens over its runs.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CachedTokens int64 `json:"cached_tokens"`
}

func turnView(t repository.Turn) Turn {
	return Turn{
		ID: t.ID, ConversationID: t.ConversationID, MemberID: t.MemberID,
		Agent: TurnAgent{ID: t.AgentID.String(), Name: t.AgentName}, Model: t.Model, Status: t.Status, Stop: t.Stop,
		Error: t.Error, Usage: Usage(t.Usage), LastSeq: t.EventSeq, StartedAt: t.StartedAt, FinishedAt: t.FinishedAt,
	}
}

// ConversationQuery picks conversations: those about records of
// SubjectKind, of SubjectID when set, or the caller's own.
type ConversationQuery struct {
	SubjectKind domain.SubjectKind
	SubjectID   *uuid.UUID
}

// NewConversation is the record a conversation is about, SubjectKind and
// SubjectID together, or neither; Key is the caller's Idempotency-Key.
type NewConversation struct {
	SubjectKind domain.SubjectKind
	SubjectID   *uuid.UUID
	Key         *uuid.UUID
}

// NewTurn is the AgentQuestion schema: the message, the console agent it
// goes to (the Console assistant when nil), and the caller's
// Idempotency-Key.
type NewTurn struct {
	Text    string
	AgentID *uuid.UUID
	Key     *uuid.UUID
}

// TurnStarted is the AgentTurnStarted schema, the 202 of a message.
type TurnStarted struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	TurnID         uuid.UUID `json:"turn_id"`
}

// TurnState is the AgentTurnState schema: the turn a control was taken
// for.
type TurnState struct {
	TurnID uuid.UUID `json:"turn_id"`
	Status string    `json:"status"`
}

// Approval is the AgentApproval schema.
type Approval struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	TurnID         uuid.UUID       `json:"turn_id"`
	MemberID       uuid.UUID       `json:"member_id"`
	Operation      string          `json:"operation"`
	Method         string          `json:"method"`
	Path           string          `json:"path"`
	Body           json.RawMessage `json:"body,omitempty"`
	Status         string          `json:"status"`
	DecidedBy      *uuid.UUID      `json:"decided_by,omitempty"`
	DecidedAt      *time.Time      `json:"decided_at,omitempty"`
	ResultStatus   *int            `json:"result_status,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

func approvalView(a repository.Approval) Approval {
	return Approval{
		ID: a.ID, ConversationID: a.ConversationID, TurnID: a.TurnID, MemberID: a.MemberID, Operation: a.Operation,
		Method: a.Method, Path: a.Path, Body: a.Body, Status: a.Status, DecidedBy: a.DecidedBy, DecidedAt: a.DecidedAt,
		ResultStatus: a.ResultStatus, Result: a.ResultBody, CreatedAt: a.CreatedAt,
	}
}

// ApprovalQuery picks approvals: of ConversationID when set, in Status
// when set.
type ApprovalQuery struct {
	ConversationID *uuid.UUID
	Status         string
}

// Request is a request as an approval keeps it to make later: Path with
// its query, the headers it replays with and its JSON body, or nil.
type Request struct {
	ID      uuid.UUID
	Method  string
	Path    string
	Headers map[string]string
	Body    json.RawMessage
}

// Replay makes a request as the member approving it and returns what it
// answered; an error is a request that could not be made, which leaves the
// approval pending.
type Replay func(req Request) (status int, body []byte, err error)
