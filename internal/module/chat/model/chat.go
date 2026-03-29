package chatmodel

import (
	chatdb "shopnexus-server/internal/module/chat/db/sqlc"

	"github.com/google/uuid"
)

// WebSocket message types
const (
	WSTypeSendMessage = "send_message"
	WSTypeNewMessage  = "new_message"
	WSTypeMarkRead    = "mark_read"
	WSTypeReadReceipt = "read_receipt"
	WSTypeError       = "error"
)

type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type WSSendMessage struct {
	ConversationID uuid.UUID              `json:"conversation_id"`
	Type           chatdb.ChatMessageType `json:"type"`
	Content        string                 `json:"content"`
	Metadata       map[string]any         `json:"metadata,omitempty"`
}

type WSMarkRead struct {
	ConversationID uuid.UUID `json:"conversation_id"`
}
