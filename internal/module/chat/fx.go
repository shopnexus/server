package chat

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	chatbiz "shopnexus-server/internal/module/chat/biz"
	chatdb "shopnexus-server/internal/module/chat/db/sqlc"
	chatecho "shopnexus-server/internal/module/chat/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

var Module = fx.Module("chat",
	fx.Provide(
		NewChatStorage,
		chatbiz.NewChatBiz,
		NewChatClient,
		chatecho.NewHandler,
	),
	fx.Invoke(
		chatecho.NewHandler,
	),
)

func NewChatStorage(pool pgsqlc.TxBeginner) chatbiz.ChatStorage {
	return pgsqlc.NewStorage(pool, chatdb.New(pool))
}

func NewChatClient(cfg *config.Config) chatbiz.ChatClient {
	return chatbiz.NewChatBizProxy(cfg.Restate.IngressAddress)
}
