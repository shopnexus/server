package chatbiz

import (
	restate "github.com/restatedev/sdk-go"

	chatdb "shopnexus-server/internal/module/chat/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

// ChatBizService wraps ChatBiz for Restate registration.
// All methods accept restate.Context and delegate to ChatBiz with context.Context.
type ChatBizService struct {
	biz *ChatBiz
}

func NewChatBizService(biz *ChatBiz) *ChatBizService {
	return &ChatBizService{biz: biz}
}

func (s *ChatBizService) CreateConversation(ctx restate.Context, params CreateConversationParams) (chatdb.ChatConversation, error) {
	return s.biz.CreateConversation(ctx, params)
}

func (s *ChatBizService) GetConversation(ctx restate.Context, id uuid.UUID) (chatdb.ChatConversation, error) {
	return s.biz.GetConversation(ctx, id)
}

func (s *ChatBizService) ListConversation(ctx restate.Context, params ListConversationParams) (sharedmodel.PaginateResult[chatdb.ChatConversation], error) {
	return s.biz.ListConversation(ctx, params)
}

func (s *ChatBizService) SendMessage(ctx restate.Context, params SendMessageParams) (chatdb.ChatMessage, error) {
	return s.biz.SendMessage(ctx, params)
}

func (s *ChatBizService) ListMessage(ctx restate.Context, params ListMessageParams) (sharedmodel.PaginateResult[chatdb.ChatMessage], error) {
	return s.biz.ListMessage(ctx, params)
}

func (s *ChatBizService) MarkRead(ctx restate.Context, params MarkReadParams) error {
	return s.biz.MarkRead(ctx, params)
}
