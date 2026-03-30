package chat

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	chatbiz "shopnexus-server/internal/module/chat/biz"
	chatdb "shopnexus-server/internal/module/chat/db/sqlc"
	chatecho "shopnexus-server/internal/module/chat/transport/echo"
	commonbiz "shopnexus-server/internal/module/common/biz"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the chat module dependencies.
var Module = fx.Module("chat",
	fx.Provide(
		NewChatStorage,
		NewChatHandler,
		NewChatBiz,
		chatecho.NewHandler,
	),
	fx.Invoke(
		chatecho.NewHandler,
	),
)

func NewChatHandler(storage chatbiz.ChatStorage, common commonbiz.CommonBiz) *chatbiz.ChatHandler {
	return chatbiz.NewChatHandler(storage, common)
}

// NewChatStorage creates a new chat storage backed by PostgreSQL.
func NewChatStorage(pool pgsqlc.TxBeginner) chatbiz.ChatStorage {
	return pgsqlc.NewStorage(pool, chatdb.New(pool))
}

// NewChatBiz creates a Restate-backed client for the chat module.
func NewChatBiz(cfg *config.Config) chatbiz.ChatBiz {
	return chatbiz.NewChatRestateClient(cfg.Restate.IngressAddress)
}
