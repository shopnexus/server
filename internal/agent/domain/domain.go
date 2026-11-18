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
