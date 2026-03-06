package chatbiz

import (
	"context"
	"encoding/json"
	"fmt"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	chatdb "shopnexus-remastered/internal/module/chat/db/sqlc"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/pgsqlc"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type ChatStorage = pgsqlc.Storage[*chatdb.Queries]

type ChatBiz struct {
	storage ChatStorage
}

func NewChatBiz(storage ChatStorage) *ChatBiz {
	return &ChatBiz{storage: storage}
}

type CreateConversationParams struct {
	Account  accountmodel.AuthenticatedAccount
	VendorID uuid.UUID `validate:"required"`
}

func (b *ChatBiz) CreateConversation(ctx context.Context, params CreateConversationParams) (chatdb.ChatConversation, error) {
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
		return zero, fmt.Errorf("failed to create conversation: %w", err)
	}

	return result, nil
}

func (b *ChatBiz) GetConversation(ctx context.Context, id uuid.UUID) (chatdb.ChatConversation, error) {
	return b.storage.Querier().GetConversation(ctx, id)
}

type ListConversationParams struct {
	Account accountmodel.AuthenticatedAccount
	sharedmodel.PaginationParams
}

func (b *ChatBiz) ListConversation(ctx context.Context, params ListConversationParams) (sharedmodel.PaginateResult[chatdb.ChatConversation], error) {
	var zero sharedmodel.PaginateResult[chatdb.ChatConversation]
	params.PaginationParams = params.Constrain()

	conversations, err := b.storage.Querier().ListConversationByAccount(ctx, chatdb.ListConversationByAccountParams{
		AccountID: params.Account.ID,
		Limit:     null.Int32From(params.Limit.Int32),
		Offset:    params.Offset(),
	})
	if err != nil {
		return zero, fmt.Errorf("failed to list conversations: %w", err)
	}

	total, err := b.storage.Querier().CountConversationByAccount(ctx, params.Account.ID)
	if err != nil {
		return zero, fmt.Errorf("failed to count conversations: %w", err)
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

func (b *ChatBiz) SendMessage(ctx context.Context, params SendMessageParams) (chatdb.ChatMessage, error) {
	var zero chatdb.ChatMessage

	conv, err := b.storage.Querier().GetConversation(ctx, params.ConversationID)
	if err != nil {
		return zero, fmt.Errorf("conversation not found: %w", err)
	}

	if conv.CustomerID != params.Account.ID && conv.VendorID != params.Account.ID {
		return zero, fmt.Errorf("not a participant of this conversation")
	}

	var msg chatdb.ChatMessage
	if err := b.storage.WithTx(ctx, nil, func(txStorage ChatStorage) error {
		var err error
		msg, err = txStorage.Querier().CreateMessage(ctx, chatdb.CreateMessageParams{
			ConversationID: params.ConversationID,
			SenderID:       params.Account.ID,
			Type:           params.Type,
			Content:        params.Content,
			Metadata:       params.Metadata,
		})
		if err != nil {
			return err
		}

		return txStorage.Querier().UpdateConversationLastMessage(ctx, params.ConversationID)
	}); err != nil {
		return zero, fmt.Errorf("failed to send message: %w", err)
	}

	return msg, nil
}

type ListMessageParams struct {
	Account        accountmodel.AuthenticatedAccount
	ConversationID uuid.UUID `validate:"required"`
	sharedmodel.PaginationParams
}

func (b *ChatBiz) ListMessage(ctx context.Context, params ListMessageParams) (sharedmodel.PaginateResult[chatdb.ChatMessage], error) {
	var zero sharedmodel.PaginateResult[chatdb.ChatMessage]
	params.PaginationParams = params.Constrain()

	conv, err := b.storage.Querier().GetConversation(ctx, params.ConversationID)
	if err != nil {
		return zero, fmt.Errorf("conversation not found: %w", err)
	}

	if conv.CustomerID != params.Account.ID && conv.VendorID != params.Account.ID {
		return zero, fmt.Errorf("not a participant of this conversation")
	}

	messages, err := b.storage.Querier().ListMessageByConversation(ctx, chatdb.ListMessageByConversationParams{
		ConversationID: params.ConversationID,
		Limit:          null.Int32From(params.Limit.Int32),
		Offset:         params.Offset(),
	})
	if err != nil {
		return zero, fmt.Errorf("failed to list messages: %w", err)
	}

	total, err := b.storage.Querier().CountMessageByConversation(ctx, params.ConversationID)
	if err != nil {
		return zero, fmt.Errorf("failed to count messages: %w", err)
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

func (b *ChatBiz) MarkRead(ctx context.Context, params MarkReadParams) error {
	return b.storage.Querier().MarkMessagesRead(ctx, chatdb.MarkMessagesReadParams{
		ConversationID: params.ConversationID,
		ReaderID:       params.Account.ID,
	})
}
