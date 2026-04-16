package chatmodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the chat module.
var (
	ErrConversationNotFound = sharedmodel.NewError(http.StatusNotFound, "The conversation does not exist")
	ErrNotParticipant       = sharedmodel.NewError(
		http.StatusForbidden,
		"You are not a participant in this conversation",
	)
)
