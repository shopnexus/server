package chatbiz

import (
	"context"

	chatdb "shopnexus-server/internal/module/chat/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

// ChatBiz is the client interface for ChatBizImpl, which is used by other modules to call ChatBizImpl methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface ChatBiz -service ChatBiz
type ChatBiz interface {
	// Conversation
	CreateConversation(ctx context.Context, params CreateConversationParams) (chatdb.ChatConversation, error)
	GetConversation(ctx context.Context, id uuid.UUID) (chatdb.ChatConversation, error)
	ListConversation(ctx context.Context, params ListConversationParams) (sharedmodel.PaginateResult[chatdb.ChatConversation], error)

	// Message
	SendMessage(ctx context.Context, params SendMessageParams) (chatdb.ChatMessage, error)
	ListMessage(ctx context.Context, params ListMessageParams) (sharedmodel.PaginateResult[chatdb.ChatMessage], error)
	MarkRead(ctx context.Context, params MarkReadParams) error
}

type ChatStorage = pgsqlc.Storage[*chatdb.Queries]

// ChatBizImpl implements the core business logic for the chat module.
type ChatBizImpl struct {
	storage ChatStorage
}

// NewChatBiz creates a new ChatBizImpl with the given dependencies.
func NewChatBiz(storage ChatStorage) *ChatBizImpl {
	return &ChatBizImpl{storage: storage}
}
