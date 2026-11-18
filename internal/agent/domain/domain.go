// Package domain holds what module agent names: the subjects of the
// console agent's conversations and reads, and the errors a console member
// sees when the agent cannot answer.
package domain

import "github.com/shopnexus/server/internal/platform/apperr"

// SubjectKind is what a conversation is about, or what a read of personal
// data was of.
type SubjectKind string

const (
	SubjectTicket  SubjectKind = "ticket"
	SubjectAccount SubjectKind = "account"
)

var (
	ErrUnavailable = apperr.New(apperr.KindUnavailable, "agent_unavailable",
		"the agent cannot be reached; the rest of the console works without it")
	ErrTimeout = apperr.New(apperr.KindTimeout, "agent_timeout", "the agent did not answer in time; ask again")
	ErrBudget  = apperr.New(apperr.KindRateLimited, "agent_budget",
		"the agent's budget for today is spent; the rest of the console works without it")
	ErrConversationNotFound = apperr.New(apperr.KindNotFound, "agent_conversation_not_found",
		"no such conversation, or not one this member may open")
	ErrRateLimited = apperr.New(apperr.KindRateLimited, "agent_rate_limited",
		"too many questions to the agent in a short time; wait and ask again")
	ErrInputRejected = apperr.New(apperr.KindInvalid, "agent_input_rejected",
		"the question is longer than 2000 characters; shorten it")
	ErrTurnInProgress = apperr.New(apperr.KindConflict, "agent_turn_in_progress",
		"the agent is still answering in this conversation; wait for it")
	ErrConversationClosed = apperr.New(apperr.KindConflict, "agent_conversation_closed",
		"the conversation is closed; start another")
)

// ErrInvalidSubject answers a subject id without its kind, and a new
// conversation with a kind and no id.
var ErrInvalidSubject = apperr.New(apperr.KindInvalid, "agent_subject_invalid",
	"a subject_id needs its subject_kind, and a new conversation takes both or neither")

// The errors of a run's controls and of what a turn made.
var (
	ErrTurnNotFound = apperr.New(apperr.KindNotFound, "agent_turn_not_found",
		"no such turn, or not one this member may open")
	ErrTurnState = apperr.New(apperr.KindConflict, "agent_turn_state",
		"the turn is not in a state that takes this: pause a running turn, continue a paused or broken one")
	ErrToolCallNotFound = apperr.New(apperr.KindNotFound, "agent_tool_call_not_found", "no such tool call")
	ErrDocumentNotFound = apperr.New(apperr.KindNotFound, "agent_document_not_found", "no such document or version")
	ErrSkillNotFound    = apperr.New(apperr.KindNotFound, "agent_skill_not_found", "no such skill")
	ErrSkillTaken       = apperr.New(apperr.KindConflict, "agent_skill_taken", "another skill has this slug")
)

var (
	ErrForbidden = apperr.New(apperr.KindForbidden, "agent_forbidden",
		"only the member who made the skill, or an admin, may change it")
	ErrAnswersInvalid = apperr.New(apperr.KindInvalid, "agent_answers_invalid",
		"the answers do not fit the turn's questions")
	ErrSkillInvalid = apperr.New(apperr.KindInvalid, "agent_skill_invalid", "the skill's fields do not fit")
	// ErrTitleInvalid refuses an empty title for a conversation.
	ErrTitleInvalid = apperr.New(apperr.KindInvalid, "agent_title_invalid", "a title has 1 to 80 characters")
)
