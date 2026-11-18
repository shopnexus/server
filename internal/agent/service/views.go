package service

import (
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
)

// The views are the schemas in api/schemas/console_agent.yaml, which are
// the agent service's own shapes: the service decodes the agent's answer
// into them and checks only what the console relies on.

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

// Turn is the AgentTurn schema: a member's question and the agent's
// answer, once it has one.
type Turn struct {
	ID       uuid.UUID `json:"id"`
	MemberID uuid.UUID `json:"member_id"`
	// Status is the agent's, in lower case: running, succeeded, failed,
	// cancelled or timed_out.
	Status    string    `json:"status"`
	Text      string    `json:"text"`
	Answer    string    `json:"answer,omitempty"`
	Sources   []Source  `json:"sources"`
	CreatedAt time.Time `json:"created_at"`
}

// Answer is the AgentAnswer schema.
type Answer struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	TurnID         uuid.UUID `json:"turn_id"`
	Answer         string    `json:"answer"`
	Sources        []Source  `json:"sources"`
}

// Source is what one tool call of a turn read: the tool, a label for the
// chip and the ids it returned.
type Source struct {
	Tool  string   `json:"tool"`
	Label string   `json:"label"`
	IDs   []string `json:"ids"`
}

// NewConversation is the record a conversation is about, SubjectKind and
// SubjectID together, or neither for a conversation of the member's own.
type NewConversation struct {
	SubjectKind domain.SubjectKind `json:"subject_kind,omitempty"`
	SubjectID   *uuid.UUID         `json:"subject_id,omitempty"`
}

// sourcesOf is in with no nil list, so the JSON has arrays.
func sourcesOf(in []Source) []Source {
	out := make([]Source, 0, len(in))
	for _, src := range in {
		if src.IDs == nil {
			src.IDs = []string{}
		}
		out = append(out, src)
	}
	return out
}
