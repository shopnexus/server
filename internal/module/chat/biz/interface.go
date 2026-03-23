package chatbiz

import (
	"context"

	chatdb "shopnexus-server/internal/module/chat/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

// ChatClient is the client interface for ChatBiz, which is used by other modules to call ChatBiz methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface ChatClient -service ChatBiz
type ChatClient interface {
	CreateConversation(ctx context.Context, params CreateConversationParams) (chatdb.ChatConversation, error)
	GetConversation(ctx context.Context, id uuid.UUID) (chatdb.ChatConversation, error)
	ListConversation(ctx context.Context, params ListConversationParams) (sharedmodel.PaginateResult[chatdb.ChatConversation], error)
	SendMessage(ctx context.Context, params SendMessageParams) (chatdb.ChatMessage, error)
	ListMessage(ctx context.Context, params ListMessageParams) (sharedmodel.PaginateResult[chatdb.ChatMessage], error)
	MarkRead(ctx context.Context, params MarkReadParams) error
}

type ChatStorage = pgsqlc.Storage[*chatdb.Queries]

type ChatBiz struct {
	storage ChatStorage
}

func NewChatBiz(storage ChatStorage) *ChatBiz {
	return &ChatBiz{storage: storage}
}
