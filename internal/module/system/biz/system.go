package systembiz

import (
	"errors"
	"shopnexus-remastered/internal/infras/pubsub"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
)

type SystemBiz struct {
	storage pgsqlc.Storage
	pubsub  pubsub.Client
}

func NewSystemBiz(
	storage pgsqlc.Storage,
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
