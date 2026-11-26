// Package repository stores module agent's record of the agent's reads of
// personal data, the AI settings, and the console agent's conversations,
// turns and what their runs made.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
)

// Read is one read of personal data an MCP tool made for a member: the
// tool, what it read and the ids it returned.
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

// The errors of Runs beside ErrNotFound and ErrTaken.
var (
	// ErrBusy is a conversation that has an unfinished turn.
	ErrBusy = errors.New("agent repository: unfinished turn")
	// ErrState is a turn not in the status the write needs.
	ErrState = errors.New("agent repository: turn state")
	// ErrKeyUsed is a create whose member's request key a row already
	// claimed; the write did nothing.
	ErrKeyUsed = errors.New("agent repository: request key used")
)

// ConversationFilter picks the conversations of a list: with SubjectKind
// those about records of that kind, of SubjectID when set; with
// MentionKind the member's own whose turns mention MentionID; with neither
// the member's own.
type ConversationFilter struct {
	SubjectKind string
	SubjectID   *uuid.UUID
	MentionKind string
	MentionID   *uuid.UUID
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

// Usage is a turn's run totals.
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
	FirstTokenMS     *int64
	Provider         string
}

// Turn is an agent_turns row, with what a read adds: whether it is its
// conversation's latest turn, and the conversation's member and subject.
type Turn struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	MemberID       uuid.UUID
	Ordinal        int
	Status         string
	Question       string
	Answer         *string
	Blocks         json.RawMessage
	Sources        json.RawMessage
	AgentID        string
	AgentName      string
	ModelAlias     string
	ErrorCode      string
	Usage          Usage
	StartedAt      time.Time
	RunStartedAt   time.Time
	DeadlineMS     int64
	FinishedAt     *time.Time
	LatencyMS      *int64
	EventSeq       int64
	Wrote          bool
	State          json.RawMessage
	// Attachments are the records the member mentioned in the question.
	Attachments []domain.Attachment
	// RequestKey is the asking member's Idempotency-Key.
	RequestKey *uuid.UUID

	Latest      bool
	Owner       uuid.UUID
	SubjectKind string
	SubjectID   *uuid.UUID
}

// Input is an agent_turn_inputs row: a pause, a stop, a steer or answers.
type Input struct {
	Seq       int64
	ID        uuid.UUID
	TurnID    uuid.UUID
	Kind      string
	Text      string
	Answers   json.RawMessage
	ReadAt    *time.Time
	CreatedAt time.Time
	// MemberID sent the input, with RequestKey, their Idempotency-Key;
	// Resumed is a steer that continued a paused run.
	MemberID   uuid.UUID
	RequestKey *uuid.UUID
	Resumed    bool
}

// Event is an agent_turn_events row.
type Event struct {
	TurnID uuid.UUID
	Seq    int64
	Type   string
	Body   json.RawMessage
	At     time.Time
}

// ToolCall is an agent_tool_calls row.
type ToolCall struct {
	ID            uuid.UUID
	TurnID        uuid.UUID
	ToolUseID     string
	Tool          string
	Label         string
	Input         json.RawMessage
	Output        *string
	OutputSpilled bool
	Status        string
	LatencyMS     *int64
	Attempts      int
	AttemptErrors json.RawMessage
	Timing        json.RawMessage
	CreatedAt     time.Time
	FinishedAt    *time.Time
}

// ToolWrite is a tool call as a run reports it; a nil field keeps what
// the row has.
type ToolWrite struct {
	ID     uuid.UUID
	TurnID uuid.UUID
	// Segment is the run_started_at of the segment that reports the call.
	Segment       time.Time
	ToolUseID     string
	Tool          string
	Label         *string
	Input         json.RawMessage
	Output        *string
	Content       json.RawMessage
	OutputSpilled *bool
	Status        string
	LatencyMS     *int64
	Attempts      *int
	AttemptErrors json.RawMessage
	Timing        json.RawMessage
	FinishedAt    *time.Time
	At            time.Time
}

// Document is an agent_documents row with its versions, newest first.
type Document struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	TurnID         *uuid.UUID
	Title          string
	LatestVersion  int
	CreatedBy      *uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Versions       []DocumentVersion
}

// DocumentVersion is an agent_document_versions row.
type DocumentVersion struct {
	Version   int
	Title     string
	Sections  json.RawMessage
	TurnID    *uuid.UUID
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}

// DocumentWrite is a write_document of a run: version 1 makes the
// document, a later one adds a version to it.
type DocumentWrite struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	TurnID         uuid.UUID
	Segment        time.Time
	MemberID       uuid.UUID
	Version        int
	Title          string
	Sections       json.RawMessage
	At             time.Time
}

// Skill is an agent_skills row.
type Skill struct {
	ID          uuid.UUID
	CreatedBy   uuid.UUID
	Name        string
	Slug        string
	Description string
	Prompt      string
	Variables   json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
	RequestKey  *uuid.UUID
	WriteID     uuid.UUID
}

// History is a terminal turn as a new run replays it, with its read
// steers, its answers and its finished tool calls in the order they came.
type History struct {
	Status   string
	Question string
	Answer   string
	Entries  []HistoryEntry
}

// HistoryEntry is an input or a tool call of a History; Text is the
// input's text or the call's output.
type HistoryEntry struct {
	Kind      string
	Text      string
	ToolUseID string
	Tool      string
	Input     json.RawMessage
	Status    string
	Content   json.RawMessage
	At        time.Time
}

// Resume makes a turn in From running again for a new segment at At.
// ClearAnswer removes a failed run's answer and blocks; Input, when set,
// is stored with it (a steer, or answers, which also clear pending_ask).
type Resume struct {
	TurnID      uuid.UUID
	From        string
	At          time.Time
	ClearAnswer bool
	Input       *Input
}

// Resumed is a resumed turn with what its segment starts from: the steers
// not read yet, the newest input seq, the conversation's documents and the
// records it mentions.
type Resumed struct {
	Turn         Turn
	Steers       []Input
	ControlAfter int64
	Documents    []DocumentRef
	Mentions     []domain.Attachment
}

// DocumentRef is a document of a conversation and its latest version.
type DocumentRef struct {
	ID            uuid.UUID
	LatestVersion int
}

// RunEnd is a segment's last line: the status it ends in (running for
// none), its answer for a final status, its totals and the closing event.
type RunEnd struct {
	TurnID    uuid.UUID
	Segment   time.Time
	Status    string
	ErrorCode string
	Answer    string
	Blocks    json.RawMessage
	Sources   json.RawMessage
	Usage     Usage
	LatencyMS int64
	Wrote     bool
	State     json.RawMessage
	EventType string
	EventBody json.RawMessage
	At        time.Time
}

// Stuck is a running turn past its segment's deadline.
type Stuck struct {
	ID           uuid.UUID
	RunStartedAt time.Time
}

// DayCount is a count for a key and a UTC day, YYYY-MM-DD.
type DayCount struct {
	Key   *string
	Date  string
	Count int
}

// ToolTotal is a tool's calls since a moment.
type ToolTotal struct {
	Name   string
	Calls  int
	Failed int
	P50MS  *float64
}

// RecentCall is a tool call for the stats' recent list, as the stats send
// it.
type RecentCall struct {
	CallID     uuid.UUID `json:"call_id"`
	TurnID     uuid.UUID `json:"turn_id"`
	MemberID   uuid.UUID `json:"member_id"`
	Tool       string    `json:"tool"`
	Label      string    `json:"label"`
	DurationMS *int64    `json:"duration_ms,omitempty"`
	Status     string    `json:"status"`
	At         time.Time `json:"at"`
}

// RuleCount is how often a guardrail rule acted, as the stats send it.
type RuleCount struct {
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

// RecentGuardrail is one guardrail event, as the stats send it.
type RecentGuardrail struct {
	Rule     string    `json:"rule"`
	Detail   string    `json:"detail"`
	MemberID uuid.UUID `json:"member_id"`
	TurnID   uuid.UUID `json:"turn_id"`
	At       time.Time `json:"at"`
}

// Runs keeps the console agent's conversations, turns, inputs, events,
// tool calls, documents and skills. Each method is its own transaction.
// A turn's event takes the turn's next event_seq and rings agent_turn.
type Runs interface {
	// OpenConversation returns c's subject's conversation, made from c the
	// first time, or inserts c when it has no subject; a request key c's
	// member already used answers that key's conversation.
	OpenConversation(ctx context.Context, c Conversation) (Conversation, error)
	// Conversations lists the conversations f picks for member,
	// newest activity first.
	Conversations(ctx context.Context, member uuid.UUID, f ConversationFilter, limit int) ([]Conversation, error)
	// Conversation reads one, ErrNotFound when it does not exist.
	Conversation(ctx context.Context, id uuid.UUID) (Conversation, error)
	// RenameConversation sets a title.
	RenameConversation(ctx context.Context, id uuid.UUID, title string, at time.Time) (Conversation, error)
	// DeleteConversation deletes one with what it holds; ErrBusy while a
	// turn of it is unfinished.
	DeleteConversation(ctx context.Context, id uuid.UUID) error

	// OpenTurn inserts t running in its conversation under the
	// conversation's lock, with the next ordinal. ErrNotFound for a
	// conversation member may not see, ErrBusy.
	OpenTurn(ctx context.Context, t Turn, member uuid.UUID) error
	// TouchConversation moves a conversation's last activity to at unless
	// it is later, and gives it question as title when it has none.
	TouchConversation(ctx context.Context, id uuid.UUID, question string, at time.Time) error
	// TurnByKey reads the turn member opened with key, or ErrNotFound.
	TurnByKey(ctx context.Context, member, key uuid.UUID) (Turn, error)
	// DeleteTurn deletes a turn whose run never started.
	DeleteTurn(ctx context.Context, id uuid.UUID) error
	// Turn reads a turn, ErrNotFound when it does not exist.
	Turn(ctx context.Context, id uuid.UUID) (Turn, error)
	// Turns lists a conversation's turns, oldest first.
	Turns(ctx context.Context, conversation uuid.UUID) ([]Turn, error)
	// Steers lists the steers of turns by turn, oldest first.
	Steers(ctx context.Context, turns []uuid.UUID) (map[uuid.UUID][]Input, error)
	// History lists a conversation's last limit terminal turns, oldest
	// first.
	History(ctx context.Context, conversation uuid.UUID, limit int) ([]History, error)
	// Documents lists a conversation's documents.
	Documents(ctx context.Context, conversation uuid.UUID) ([]DocumentRef, error)
	// Mentions lists the records the turns of turn's conversation mention,
	// each once without its label: turn's first, then the newest turn's.
	Mentions(ctx context.Context, turn uuid.UUID, limit int) ([]domain.Attachment, error)
	// ResumeTurn runs r; ErrState when the turn is not in r.From, or a
	// failed one is no longer its conversation's latest or another turn
	// is unfinished.
	ResumeTurn(ctx context.Context, r Resume) (Resumed, error)
	// RestoreTurn puts a turn ResumeTurn made running back as before was,
	// and deletes the input stored with it.
	RestoreTurn(ctx context.Context, before Turn, input *uuid.UUID) error
	// CancelTurn ends a turn in from as cancelled with sources and the
	// event body; ErrState when it is no longer in from.
	CancelTurn(ctx context.Context, id uuid.UUID, from string, sources, body json.RawMessage, at time.Time) error
	// CloseStuck closes a turn still running the segment that started at
	// segment as failed with code, answer and turn.finished's body; false
	// when it no longer runs that segment.
	CloseStuck(ctx context.Context, id uuid.UUID, segment time.Time, code, answer string, body json.RawMessage, at time.Time) (bool, error)
	// StuckTurns lists the turns running past their segment's deadline and
	// grace at at.
	StuckTurns(ctx context.Context, at time.Time, grace time.Duration, limit int) ([]Stuck, error)

	// AddInput stores in for a running turn under the turn's row lock, so
	// inputs commit in seq order; ErrState when it is not running,
	// ErrKeyUsed when in's request key has an input.
	AddInput(ctx context.Context, in Input) error
	// InputByKey reads the input member sent with key, or ErrNotFound.
	InputByKey(ctx context.Context, member, key uuid.UUID) (Input, error)
	// Control lists a turn's pauses, stops and steers after seq, and
	// whether it runs; a missing turn does not.
	Control(ctx context.Context, turn uuid.UUID, after int64, limit int) ([]Input, bool, error)

	// AppendEvent writes an event whatever the turn's status.
	AppendEvent(ctx context.Context, turn uuid.UUID, typ string, body json.RawMessage, at time.Time) (Event, error)
	// The writes of a run below take the segment that started at segment
	// (or the write's Segment) and change nothing once the turn no longer
	// runs that segment, so a late stream cannot write into a resumed turn.

	// AppendRunEvent writes a run's event, marks the turn wrote when wrote
	// is set, and marks the steer read when steer is set; false when the
	// turn no longer runs the segment.
	AppendRunEvent(ctx context.Context, turn uuid.UUID, segment time.Time, typ string, body json.RawMessage, wrote bool,
		steer *uuid.UUID, at time.Time) (bool, error)
	// Events lists a turn's events after seq, at most limit.
	Events(ctx context.Context, turn uuid.UUID, after int64, limit int) ([]Event, error)
	// Checkpoint stores a running segment's state.
	Checkpoint(ctx context.Context, turn uuid.UUID, segment time.Time, state json.RawMessage, wrote bool) error
	// SaveToolCall upserts w while its segment runs; a finished call does
	// not go back to pending or running.
	SaveToolCall(ctx context.Context, w ToolWrite) error
	// WriteDocument stores a write while its segment runs; false when it
	// does not, or the document is not the conversation's or its version
	// is not next.
	WriteDocument(ctx context.Context, w DocumentWrite) (bool, error)
	// FinishRun applies e to a turn running e's segment and writes its
	// event; false when the turn no longer runs it.
	FinishRun(ctx context.Context, e RunEnd) (bool, error)

	// BlankOutputsBefore blanks the output and result content of at most
	// limit tool calls made before before, and returns how many.
	BlankOutputsBefore(ctx context.Context, before time.Time, limit int) (int, error)
	// BlankAccountOutputs blanks the outputs and result contents of the
	// tool calls in the conversation about account.
	BlankAccountOutputs(ctx context.Context, account uuid.UUID) error
	// DeleteIdleConversations deletes at most limit conversations with no
	// activity since before, with all they hold, and returns how many.
	DeleteIdleConversations(ctx context.Context, before time.Time, limit int) (int, error)

	// ToolCall reads a tool call of a conversation member may see.
	ToolCall(ctx context.Context, id, member uuid.UUID) (ToolCall, error)
	// Document reads a document of a conversation member may see.
	Document(ctx context.Context, id, member uuid.UUID) (Document, error)

	// Skills lists every skill by name.
	Skills(ctx context.Context) ([]Skill, error)
	// Skill reads one.
	Skill(ctx context.Context, id uuid.UUID) (Skill, error)
	// CreateSkill inserts sk; ErrTaken for a slug in use, ErrKeyUsed when
	// its creator's request key has a skill.
	CreateSkill(ctx context.Context, sk Skill) error
	// SkillByKey reads the skill member made with key, or ErrNotFound.
	SkillByKey(ctx context.Context, member, key uuid.UUID) (Skill, error)
	// UpdateSkill writes sk over the row read with readWriteID, which sk's
	// WriteID replaces; ErrNotFound when it is gone, ErrConflict when it
	// changed, ErrTaken for a slug in use.
	UpdateSkill(ctx context.Context, sk Skill, readWriteID uuid.UUID) error
	// DeleteSkill deletes one.
	DeleteSkill(ctx context.Context, id uuid.UUID) error

	// The stats since from.
	ToolTotals(ctx context.Context, from time.Time) ([]ToolTotal, error)
	ToolDays(ctx context.Context, from time.Time) ([]DayCount, error)
	RecentCalls(ctx context.Context, from time.Time, limit int) ([]RecentCall, error)
	GuardrailRules(ctx context.Context, from time.Time) ([]RuleCount, error)
	RecentGuardrails(ctx context.Context, from time.Time, limit int) ([]RecentGuardrail, error)
	AgentDays(ctx context.Context, from time.Time) ([]DayCount, error)

	// ListenTurns calls ring with each turn agent_turn rings for until ctx
	// is done, and resync each time it starts to listen.
	ListenTurns(ctx context.Context, ring func(uuid.UUID), resync func())
}
