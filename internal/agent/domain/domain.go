// Package domain holds what module agent names: the console agent's
// conversations and turns, the capabilities that let an agent call the
// API, and the errors a console member sees.
package domain

import (
	"regexp"
	"strings"

	"github.com/shopnexus/server/internal/platform/apperr"
)

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
	ErrConversationNotFound = apperr.New(apperr.KindNotFound, "agent_conversation_not_found",
		"no such conversation, or not one this member may open")
	ErrRateLimited = apperr.New(apperr.KindRateLimited, "agent_rate_limited",
		"too many messages to the agent in a short time; wait and write again")
	ErrInputRejected  = apperr.New(apperr.KindInvalid, "agent_input_rejected", "the message is empty")
	ErrTurnInProgress = apperr.New(apperr.KindConflict, "agent_turn_in_progress",
		"the agent is still working in this conversation; wait for it, pause it or write to it")
	// ErrInvalidSubject answers a subject id without its kind, and a new
	// conversation with a kind and no id.
	ErrInvalidSubject = apperr.New(apperr.KindInvalid, "agent_subject_invalid",
		"a subject_id needs its subject_kind, and a new conversation takes both or neither")
	ErrTurnNotFound = apperr.New(apperr.KindNotFound, "agent_turn_not_found",
		"no such turn, or not one this member may open")
	ErrTurnState = apperr.New(apperr.KindConflict, "agent_turn_state",
		"the turn is not in a state that takes this: pause, stop and steer a running turn, resume the last one once it ended")
	ErrForbidden = apperr.New(apperr.KindForbidden, "agent_forbidden",
		"a conversation shared by every member is an admin's to rename or delete")
	// ErrTitleInvalid refuses an empty title for a conversation.
	ErrTitleInvalid     = apperr.New(apperr.KindInvalid, "agent_title_invalid", "a title has 1 to 80 characters")
	ErrRequestInvalid   = apperr.New(apperr.KindBadRequest, "invalid_request", "the request does not fit the agent")
	ErrApprovalNotFound = apperr.New(apperr.KindNotFound, "agent_approval_not_found",
		"no such approval, or one of a conversation this member may not open")
	ErrApprovalDecided = apperr.New(apperr.KindConflict, "agent_approval_decided", "the approval was already decided")
)

// The errors an agent's turn token meets.
var (
	ErrTokenInvalid = apperr.New(apperr.KindUnauthenticated, "agent_token_invalid",
		"the turn's token is unknown or its run has ended")
	ErrOperationForbidden = apperr.New(apperr.KindForbidden, "agent_operation_forbidden",
		"this agent holds no capability that grants the operation")
)

var (
	zeroWidth = regexp.MustCompile(`[\x{200B}-\x{200D}\x{FEFF}]`)
	controls  = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
)

// CleanInput returns a member's message without zero-width and control
// characters (tabs and newlines stay). A message empty once cleaned is
// ErrInputRejected. A member is trusted, so phrases that read as
// instructions pass; the spec bounds the length.
func CleanInput(text string) (string, error) {
	clean := controls.ReplaceAllString(zeroWidth.ReplaceAllString(text, ""), "")
	if strings.TrimSpace(clean) == "" {
		return "", ErrInputRejected
	}
	return clean, nil
}
