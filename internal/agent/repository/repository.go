// Package repository stores module agent's record of the agent's reads of
// personal data, the AI settings, and the console agent's conversations:
// their messages, turns, events, approvals and API calls.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
)

// Read is one read of personal data made for a member: what read it, what
// it read and the ids it returned.
type Read struct {
	ID          uuid.UUID
	MemberID    uuid.UUID
	Tool        string
	SubjectKind domain.SubjectKind
	SubjectIDs  []uuid.UUID
	At          time.Time
}

// Reads keeps console_agent_reads. Each method is its own statement.
type Reads interface {
	// Insert stores r; the same id again does nothing.
	Insert(ctx context.Context, r Read) error
	// DeleteBefore deletes at most limit reads made earlier than before,
	// and returns how many it deleted.
	DeleteBefore(ctx context.Context, before time.Time, limit int) (int, error)
}

// The errors of Conversations beside ErrNotFound.
var (
	// ErrBusy is a conversation that has a turn running.
	ErrBusy = errors.New("agent repository: turn running")
	// ErrState is a turn or an approval not in the status the write needs.
	ErrState = errors.New("agent repository: state")
	// ErrKeyUsed is a create whose member's request key a row already
	// claimed; the write did nothing.
	ErrKeyUsed = errors.New("agent repository: request key used")
)

// ConversationFilter picks the conversations of a list: with SubjectKind
// those about records of that kind, of SubjectID when set; without, the
// member's own.
type ConversationFilter struct {
	SubjectKind string
	SubjectID   *uuid.UUID
}

// Conversation is an agent_conversations row. SubjectKind and SubjectID
// are both set or both empty.
type Conversation struct {
	ID             uuid.UUID
	MemberID       uuid.UUID
	SubjectKind    string
	SubjectID      *uuid.UUID
	Title          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastActivityAt time.Time
	// RequestKey is the creator's Idempotency-Key, kept for a conversation
	// without a subject.
	RequestKey *uuid.UUID
}

// Message is an agent_messages row: Body is a domain.Message as JSON.
type Message struct {
	Seq    int
	TurnID *uuid.UUID
	Body   json.RawMessage
	At     time.Time
}

// Usage is a turn's token totals over its runs.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
}

// Turn is an agent_turns row, with what a read adds: whether it is its
// conversation's last turn, and the conversation's member and subject.
type Turn struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	MemberID       uuid.UUID
	AgentID        uuid.UUID
	AgentName      string
	Model          string
	Capabilities   []string
	Status         string
	Stop           string
	Error          string
	// TokenHash is the SHA-256 of the running run's token.
	TokenHash    []byte
	Usage        Usage
	EventSeq     int64
	RequestKey   *uuid.UUID
	StartedAt    time.Time
	RunStartedAt time.Time
	DeadlineAt   time.Time
	FinishedAt   *time.Time

	Latest      bool
	Owner       uuid.UUID
	SubjectKind string
	SubjectID   *uuid.UUID
}

// Resume makes the conversation's last turn running again.
type Resume struct {
	TurnID       uuid.UUID
	TokenHash    []byte
	RunStartedAt time.Time
	DeadlineAt   time.Time
}

// RunEnd is how a run ended: the turn's status after it, the runtime's
// stop reason and error, and the run's usage, which adds to the turn's.
type RunEnd struct {
	TurnID       uuid.UUID
	RunStartedAt time.Time
	Status       string
	Stop         string
	Error        string
	Usage        Usage
	At           time.Time
}

// Event is an agent_turn_events row.
type Event struct {
	TurnID uuid.UUID
	Seq    int64
	Type   string
	Body   json.RawMessage
	At     time.Time
}

// Approval is an agent_approvals row. Headers are the request headers the
// call replays with; Body and ResultBody are JSON or nil.
type Approval struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	TurnID         uuid.UUID
	MemberID       uuid.UUID
	Operation      string
	Method         string
	Path           string
	Headers        map[string]string
	Body           json.RawMessage
	Status         string
	DecidedBy      *uuid.UUID
	DecidedAt      *time.Time
	ResultStatus   *int
	ResultBody     json.RawMessage
	CreatedAt      time.Time
}

// Decision is what a member decided on a pending approval.
type Decision struct {
	Status       string
	By           uuid.UUID
	At           time.Time
	ResultStatus *int
	ResultBody   json.RawMessage
}

// ApprovalFilter picks the approvals of the conversations Member may
// open: of ConversationID when set, in Status when set, newest first.
type ApprovalFilter struct {
	Member         uuid.UUID
	ConversationID *uuid.UUID
	Status         string
	Limit          int
}

// Call is one API call a turn's token made.
type Call struct {
	TurnID    uuid.UUID
	Operation string
	Method    string
	Path      string
	Status    int
	TookMS    int64
	At        time.Time
}

// Stuck is a running turn past its deadline.
type Stuck struct {
	TurnID       uuid.UUID
	RunStartedAt time.Time
}

// DayCount is a count for a key and a UTC day, YYYY-MM-DD.
type DayCount struct {
	Key   *string
	Date  string
	Count int
}

// Conversations keeps the console agent's conversations with their
// messages, turns, events, approvals and calls.
type Conversations interface {
	// OpenConversation stores c, or returns the conversation of its
	// subject, or of its member's request key, that exists.
	OpenConversation(ctx context.Context, c Conversation) (Conversation, error)
	// Conversations lists the conversations f picks for member, newest
	// activity first.
	Conversations(ctx context.Context, member uuid.UUID, f ConversationFilter, limit int) ([]Conversation, error)
	// Conversation reads one; ErrNotFound when there is none.
	Conversation(ctx context.Context, id uuid.UUID) (Conversation, error)
	RenameConversation(ctx context.Context, id uuid.UUID, title string, at time.Time) (Conversation, error)
	// DeleteConversation deletes one with what it holds; ErrBusy while a
	// turn runs.
	DeleteConversation(ctx context.Context, id uuid.UUID) error
	// Messages reads a conversation's history in seq order.
	Messages(ctx context.Context, conversation uuid.UUID) ([]Message, error)
	// Turns reads a conversation's turns, oldest first.
	Turns(ctx context.Context, conversation uuid.UUID) ([]Turn, error)

	// OpenTurn stores t running and touches the conversation, giving it
	// title when it has none. ErrBusy when a turn of the conversation
	// runs, ErrKeyUsed when t's request key was used, ErrNotFound without
	// the conversation.
	OpenTurn(ctx context.Context, t Turn, title string, at time.Time) error
	// ResumeTurn makes the conversation's last turn running again: ErrState
	// when it is running or not the last.
	ResumeTurn(ctx context.Context, res Resume, at time.Time) error
	// DeleteTurn deletes a turn whose run did not start; ResetTurn returns
	// a resumed one to before, as its run did not start either.
	DeleteTurn(ctx context.Context, id uuid.UUID) error
	ResetTurn(ctx context.Context, before Turn) error
	// Turn reads one with its conversation's owner and subject.
	Turn(ctx context.Context, id uuid.UUID) (Turn, error)
	// TurnByKey reads the turn member opened with key.
	TurnByKey(ctx context.Context, member, key uuid.UUID) (Turn, error)
	// TurnByToken reads the running turn whose token hashes to hash and
	// whose deadline is after at.
	TurnByToken(ctx context.Context, hash []byte, at time.Time) (Turn, error)
	// AppendMessages adds msgs to turn's conversation's history, when the
	// turn's run started at run still runs; false when it does not.
	AppendMessages(ctx context.Context, turn uuid.UUID, run time.Time, msgs []json.RawMessage, at time.Time) (bool, error)
	// AppendEvent adds an event to a turn and rings its streams.
	AppendEvent(ctx context.Context, turn uuid.UUID, typ string, body json.RawMessage, at time.Time) (Event, error)
	// Events reads a turn's events after seq after.
	Events(ctx context.Context, turn uuid.UUID, after int64, limit int) ([]Event, error)
	// FinishRun ends the run e names, with the event typ, once: false when
	// the run had ended.
	FinishRun(ctx context.Context, e RunEnd, typ string, body json.RawMessage) (bool, error)
	// StuckTurns lists running turns whose deadline passed grace before at.
	StuckTurns(ctx context.Context, at time.Time, grace time.Duration, limit int) ([]Stuck, error)
	// ListenTurns calls ring with the turn of every new event until ctx is
	// done, and resync when rings may have been lost.
	ListenTurns(ctx context.Context, ring func(uuid.UUID), resync func())

	// AddApproval stores a pending approval.
	AddApproval(ctx context.Context, a Approval) error
	Approval(ctx context.Context, id uuid.UUID) (Approval, error)
	Approvals(ctx context.Context, f ApprovalFilter) ([]Approval, error)
	// Decide locks a pending approval, calls decide and stores its
	// decision, so two members never both decide one; ErrState when it was
	// decided.
	Decide(ctx context.Context, id uuid.UUID, decide func(Approval) (Decision, error)) (Approval, error)
	// Unreported lists a conversation's decided approvals no turn has
	// told the agent of, oldest first.
	Unreported(ctx context.Context, conversation uuid.UUID) ([]Approval, error)
	// MarkReported notes that a turn told the agent of approvals ids.
	MarkReported(ctx context.Context, ids []uuid.UUID, at time.Time) error

	// RecordCall stores a call a turn's token made.
	RecordCall(ctx context.Context, c Call) error

	// BlankOutputsBefore blanks at most limit command outputs (tool
	// messages and tool.finished events) older than before, and returns
	// how many it blanked.
	BlankOutputsBefore(ctx context.Context, before time.Time, limit int) (int, error)
	// BlankAccountOutputs blanks the command outputs in the conversation
	// about an account.
	BlankAccountOutputs(ctx context.Context, account uuid.UUID) error
	// DeleteIdleConversations deletes at most limit conversations idle
	// since before.
	DeleteIdleConversations(ctx context.Context, before time.Time, limit int) (int, error)
	// DeleteCallsBefore deletes at most limit calls older than before.
	DeleteCallsBefore(ctx context.Context, before time.Time, limit int) (int, error)
	// AgentDays counts each agent's turns per UTC day since from.
	AgentDays(ctx context.Context, from time.Time) ([]DayCount, error)
}
