package chatmodel

import sharedmodel "shopnexus-server/internal/shared/model"

var (
	ErrConversationNotFound = sharedmodel.NewError("chat.conversation_not_found", "The conversation does not exist")
	ErrNotParticipant       = sharedmodel.NewError("chat.not_participant", "You are not a participant in this conversation")
)
