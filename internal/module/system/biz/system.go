package systembiz

import (
	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgsqlc"
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

	return b, errutil.Some(
		b.SetupPubsub(),
	)
}
