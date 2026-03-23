package systembiz

import (
	"errors"
	"shopnexus-server/internal/infras/pubsub"
	systemdb "shopnexus-server/internal/module/system/db/sqlc"
	"shopnexus-server/internal/shared/pgsqlc"
)

type SystemStorage = pgsqlc.Storage[*systemdb.Queries]

type SystemBiz struct {
	storage SystemStorage
	pubsub  pubsub.Client
}

func NewSystemBiz(
	storage SystemStorage,
	pubsub pubsub.Client,
) (*SystemBiz, error) {
	b := &SystemBiz{
		storage: storage,
		pubsub:  pubsub.Group("system"),
	}

	return b, errors.Join(
		b.SetupPubsub(),
	)
}
