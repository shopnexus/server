package chatbiz

import (
	"encoding/json"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	accountmodel "shopnexus-server/internal/module/account/model"
	chatdb "shopnexus-server/internal/module/chat/db/sqlc"
	chatmodel "shopnexus-server/internal/module/chat/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type CreateConversationParams struct {
	Account  accountmodel.AuthenticatedAccount
	VendorID uuid.UUID `validate:"required"`
}

// CreateConversation creates a new conversation between a customer and vendor, or returns the existing one.
func (b *ChatHandler) CreateConversation(ctx restate.Context, params CreateConversationParams) (chatdb.ChatConversation, error) {
	var zero chatdb.ChatConversation

	existing, err := b.storage.Querier().GetConversationByParticipants(ctx, chatdb.GetConversationByParticipantsParams{
		CustomerID: params.Account.ID,
		VendorID:   params.VendorID,
	})

	if err == nil {
		return existing, nil
	}

	result, err := b.storage.Querier().CreateConversation(ctx, chatdb.CreateConversationParams{
		CustomerID: params.Account.ID,
		VendorID:   params.VendorID,
	})
	if err != nil {
		return zero, fmt.Errorf("create conversation: %w", err)
	}

	return result, nil
}

// GetConversation returns a conversation by its ID.
func (b *ChatHandler) GetConversation(ctx restate.Context, id uuid.UUID) (chatdb.ChatConversation, error) {
	return b.storage.Querier().GetConversationByID(ctx, id)
}

type ListConversationParams struct {
	Account accountmodel.AuthenticatedAccount
	sharedmodel.PaginationParams
}

// ListConversation returns a paginated list of conversations for the authenticated account.
func (b *ChatHandler) ListConversation(ctx restate.Context, params ListConversationParams) (sharedmodel.PaginateResult[chatdb.ChatConversation], error) {
	var zero sharedmodel.PaginateResult[chatdb.ChatConversation]
	params.PaginationParams = params.Constrain()

	conversations, err := b.storage.Querier().ListConversationByAccount(ctx, chatdb.ListConversationByAccountParams{
		AccountID: params.Account.ID,
		Limit:     int32(params.Limit.Int32),
		Offset:    int32(params.Offset().Int32),
	})
	if err != nil {
		return zero, fmt.Errorf("list conversations: %w", err)
	}

	total, err := b.storage.Querier().CountConversationByAccount(ctx, params.Account.ID)
	if err != nil {
		return zero, fmt.Errorf("count conversations: %w", err)
	}

	return sharedmodel.PaginateResult[chatdb.ChatConversation]{
		PageParams: params.PaginationParams,
		Data:       conversations,
		Total:      null.IntFrom(total),
	}, nil
}

type SendMessageParams struct {
	Account        accountmodel.AuthenticatedAccount
	ConversationID uuid.UUID              `validate:"required"`
	Type           chatdb.ChatMessageType `validate:"required,validateFn=Valid"`
	Content        string                 `validate:"required"`
	Metadata       json.RawMessage
}

// SendMessage sends a message in a conversation the account participates in.
func (b *ChatHandler) SendMessage(ctx restate.Context, params SendMessageParams) (chatdb.ChatMessage, error) {
	var zero chatdb.ChatMessage

	conv, err := b.storage.Querier().GetConversationByID(ctx, params.ConversationID)
	if err != nil {
		return zero, chatmodel.ErrConversationNotFound.Terminal()
	}

	if conv.CustomerID != params.Account.ID && conv.VendorID != params.Account.ID {
		return zero, chatmodel.ErrNotParticipant.Terminal()
	}

	msg, err := b.storage.Querier().CreateChatMessage(ctx, chatdb.CreateChatMessageParams{
		ConversationID: params.ConversationID,
		SenderID:       params.Account.ID,
		Type:           params.Type,
		Content:        params.Content,
		Metadata:       params.Metadata,
	})
	if err != nil {
		return zero, fmt.Errorf("create message: %w", err)
	}

	if err := b.storage.Querier().UpdateConversationLastMessage(ctx, params.ConversationID); err != nil {
		return zero, fmt.Errorf("update conversation last message: %w", err)
	}

	return msg, nil
}

type ListMessageParams struct {
	Account        accountmodel.AuthenticatedAccount
	ConversationID uuid.UUID `validate:"required"`
	sharedmodel.PaginationParams
}

// ListMessage returns a paginated list of messages in a conversation.
func (b *ChatHandler) ListMessage(ctx restate.Context, params ListMessageParams) (sharedmodel.PaginateResult[chatdb.ChatMessage], error) {
	var zero sharedmodel.PaginateResult[chatdb.ChatMessage]
	params.PaginationParams = params.Constrain()

	conv, err := b.storage.Querier().GetConversationByID(ctx, params.ConversationID)
	if err != nil {
		return zero, chatmodel.ErrConversationNotFound.Terminal()
	}

	if conv.CustomerID != params.Account.ID && conv.VendorID != params.Account.ID {
		return zero, chatmodel.ErrNotParticipant.Terminal()
	}

	messages, err := b.storage.Querier().ListMessageByConversation(ctx, chatdb.ListMessageByConversationParams{
		ConversationID: params.ConversationID,
		Limit:          int32(params.Limit.Int32),
		Offset:         int32(params.Offset().Int32),
	})
	if err != nil {
		return zero, fmt.Errorf("list messages: %w", err)
	}

	total, err := b.storage.Querier().CountMessageByConversation(ctx, params.ConversationID)
	if err != nil {
		return zero, fmt.Errorf("count messages: %w", err)
	}

	return sharedmodel.PaginateResult[chatdb.ChatMessage]{
		PageParams: params.PaginationParams,
		Data:       messages,
		Total:      null.IntFrom(total),
	}, nil
}

type MarkReadParams struct {
	Account        accountmodel.AuthenticatedAccount
	ConversationID uuid.UUID `validate:"required"`
}

// MarkRead marks all messages in a conversation as read for the authenticated account.
func (b *ChatHandler) MarkRead(ctx restate.Context, params MarkReadParams) error {
	return b.storage.Querier().MarkMessagesRead(ctx, chatdb.MarkMessagesReadParams{
		ConversationID: params.ConversationID,
		ReaderID:       params.Account.ID,
	})
}
