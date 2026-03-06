package chat

import (
	"go.uber.org/fx"

	chatbiz "shopnexus-remastered/internal/module/chat/biz"
	chatdb "shopnexus-remastered/internal/module/chat/db/sqlc"
	chatecho "shopnexus-remastered/internal/module/chat/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

var Module = fx.Module("chat",
	fx.Provide(
		NewChatStorage,
		chatbiz.NewChatBiz,
		chatecho.NewHandler,
	),
	fx.Invoke(
		chatecho.NewHandler,
	),
)

func NewChatStorage(pool pgsqlc.TxBeginner) chatbiz.ChatStorage {
	return pgsqlc.NewStorage(pool, chatdb.New(pool))
}
