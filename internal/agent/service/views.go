package service

import (
	"encoding/json"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
)

// The views are the schemas in api/schemas/console_agent.yaml, which are
// the agent service's own shapes: the service decodes the agent's answer
// into them and checks only what the console relies on. A part the agent
// owns whole, such as an answer block or a tool's input, passes through
// as raw JSON.

// Conversation is the AgentConversation schema.
type Conversation struct {
	ID          uuid.UUID          `json:"id"`
	SubjectKind domain.SubjectKind `json:"subject_kind,omitempty"`
	SubjectID   *uuid.UUID         `json:"subject_id,omitempty"`
	Title       string             `json:"title,omitempty"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// ConversationDetail is the AgentConversationDetail schema: a
// conversation with its turns, oldest first.
type ConversationDetail struct {
	Conversation

	Turns []Turn `json:"turns"`
}

// Turn is the AgentTurn schema, the agent's turn view: a member's question,
// its run's state and the answer, once it has one.
type Turn struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	MemberID       uuid.UUID  `json:"member_id"`
	// Status is the agent's, in lower case: running, paused,
	// awaiting_input, succeeded, failed, cancelled, timed_out or budget.
	Status  string            `json:"status"`
	Text    string            `json:"text"`
	Answer  string            `json:"answer,omitempty"`
	Blocks  []json.RawMessage `json:"blocks"`
	Sources []Source          `json:"sources"`
	// Outcome is the group a turn's end falls in, empty on success:
	// stopped, out_of_room, broken, unavailable or blocked.
	Outcome     string     `json:"outcome,omitempty"`
	ErrorCode   string     `json:"error_code,omitempty"`
	Continuable bool       `json:"continuable"`
	Agent       *TurnAgent `json:"agent,omitempty"`
	Usage       *Usage     `json:"usage,omitempty"`
	Steers      []Steer    `json:"steers"`
	// Questions are the clarifying questions while the turn is
	// awaiting_input.
	Questions []json.RawMessage `json:"questions,omitempty"`
	LastSeq   int               `json:"last_seq"`
	CreatedAt time.Time         `json:"created_at"`
}

// lists is t with its status in lower case and no nil list.
func (t Turn) lists() Turn {
	t.Status = strings.ToLower(t.Status)
	if t.Blocks == nil {
		t.Blocks = []json.RawMessage{}
	}
	if t.Sources == nil {
		t.Sources = []Source{}
	}
	if t.Steers == nil {
		t.Steers = []Steer{}
	}
	return t
}

// TurnAgent is the agent that ran a turn.
type TurnAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Steer is an update a member sent a run.
type Steer struct {
	SteerID uuid.UUID `json:"steer_id"`
	Text    string    `json:"text"`
}

// Usage is a turn's usage summed over its model calls.
type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	FirstTokenMS *int    `json:"first_token_ms,omitempty"`
	Provider     string  `json:"provider,omitempty"`
}

// Source is a numbered record a tool call of the turn returned, which the
// answer's [n] markers cite: its kind and id, a label, the line quoted,
// the call and its tool.
type Source struct {
	N        int       `json:"n"`
	Kind     string    `json:"kind"`
	RecordID string    `json:"record_id"`
	Label    string    `json:"label"`
	Quote    string    `json:"quote,omitempty"`
	CallID   uuid.UUID `json:"call_id"`
	Tool     string    `json:"tool"`
}

// NewConversation is the record a conversation is about, SubjectKind and
// SubjectID together, or neither for a conversation of the member's own.
type NewConversation struct {
	SubjectKind domain.SubjectKind `json:"subject_kind,omitempty"`
	SubjectID   *uuid.UUID         `json:"subject_id,omitempty"`
}

// NewTurn is the AgentQuestion schema: the question, the console agent
// that answers it (the Console assistant when nil), the records attached
// and the skill it comes from.
type NewTurn struct {
	Text        string       `json:"text"`
	AgentID     *uuid.UUID   `json:"agent_id,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	SkillID     *uuid.UUID   `json:"skill_id,omitempty"`
}

// Attachment is a record or a document a question is about.
type Attachment struct {
	Kind  string    `json:"kind"`
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label,omitempty"`
}

// AgentConfig is what the agent service runs a turn with, from ai_agents.
type AgentConfig struct {
	ID                  uuid.UUID     `json:"id"`
	Name                string        `json:"name"`
	Alias               string        `json:"alias"`
	Instructions        string        `json:"instructions"`
	InstructionsVersion int           `json:"instructions_version"`
	Tools               []string      `json:"tools"`
	Limits              domain.Limits `json:"limits"`
}

// TurnStarted is the AgentTurnStarted schema, the 202 of a question.
type TurnStarted struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	TurnID         uuid.UUID `json:"turn_id"`
}

// TurnState is the AgentTurnState schema: the turn a control was taken
// for, and its status when the control set one (end sets cancelled).
type TurnState struct {
	TurnID uuid.UUID `json:"turn_id"`
	Status string    `json:"status,omitempty"`
}

// Steering is the AgentSteering schema: the update the run will read, and
// whether it continued a paused run.
type Steering struct {
	SteerID uuid.UUID `json:"steer_id"`
	TurnID  uuid.UUID `json:"turn_id"`
	Resumed bool      `json:"resumed"`
}

// Answers are the member's answers to a turn's clarifying questions: a
// question's id to an option's id, or to a list of them for a multi
// question.
type Answers map[string]json.RawMessage

// ToolCall is the AgentToolCall schema.
type ToolCall struct {
	ID            uuid.UUID       `json:"id"`
	TurnID        uuid.UUID       `json:"turn_id"`
	Tool          string          `json:"tool"`
	Status        string          `json:"status"`
	Input         json.RawMessage `json:"input"`
	Output        json.RawMessage `json:"output,omitempty"`
	OutputSpilled bool            `json:"output_spilled"`
	Attempts      int             `json:"attempts"`
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at,omitempty"`
	DurationMS    *int            `json:"duration_ms,omitempty"`
	Timing        *Timing         `json:"timing,omitempty"`
	// AttemptErrors are the errors of the attempts that failed before the
	// last.
	AttemptErrors []AttemptError `json:"attempt_errors,omitempty"`
}

// AttemptError is a failed attempt of a tool call.
type AttemptError struct {
	Attempt int       `json:"attempt"`
	Error   string    `json:"error"`
	At      time.Time `json:"at"`
}

// Timing splits a tool call's time in milliseconds; DB is what the server
// reported, when it did.
type Timing struct {
	Queued *int `json:"queued"`
	Server *int `json:"server"`
	DB     *int `json:"db"`
	Return *int `json:"return"`
}

// Document is the AgentDocument schema: a document a turn wrote, with
// every version, newest first.
type Document struct {
	ID             uuid.UUID         `json:"id"`
	ConversationID uuid.UUID         `json:"conversation_id"`
	TurnID         *uuid.UUID        `json:"turn_id,omitempty"`
	Title          string            `json:"title"`
	LatestVersion  int               `json:"latest_version"`
	CreatedBy      *uuid.UUID        `json:"created_by,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	Versions       []DocumentVersion `json:"versions"`
}

// DocumentVersion is one version of a document and the turn that wrote it.
type DocumentVersion struct {
	Version   int               `json:"version"`
	Title     string            `json:"title"`
	Sections  []json.RawMessage `json:"sections"`
	TurnID    *uuid.UUID        `json:"turn_id,omitempty"`
	CreatedBy *uuid.UUID        `json:"created_by,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// Skill is the AgentSkill schema: a saved question with its variables,
// shared with every member.
type Skill struct {
	ID          uuid.UUID       `json:"id"`
	CreatedBy   uuid.UUID       `json:"created_by"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description"`
	Prompt      string          `json:"prompt"`
	Variables   []SkillVariable `json:"variables"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// lists is sk with no nil list, so the JSON has arrays.
func (sk Skill) lists() Skill {
	if sk.Variables == nil {
		sk.Variables = []SkillVariable{}
	}
	return sk
}

// SkillVariable is a blank in a skill's prompt, {{name}}.
type SkillVariable struct {
	Name    string `json:"name"`
	Label   string `json:"label,omitempty"`
	Default string `json:"default,omitempty"`
}

// SkillInput is a new skill; the agent makes the slug from the name when
// it has none.
type SkillInput struct {
	Name        string          `json:"name"`
	Slug        string          `json:"slug,omitempty"`
	Description string          `json:"description,omitempty"`
	Prompt      string          `json:"prompt"`
	Variables   []SkillVariable `json:"variables,omitempty"`
}

// SkillPatch changes the fields it sets.
type SkillPatch struct {
	Name        *string          `json:"name,omitempty"`
	Slug        *string          `json:"slug,omitempty"`
	Description *string          `json:"description,omitempty"`
	Prompt      *string          `json:"prompt,omitempty"`
	Variables   *[]SkillVariable `json:"variables,omitempty"`
}
