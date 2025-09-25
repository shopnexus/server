package systembiz

import (
	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgutil"
)

type SystemBiz struct {
	storage *pgutil.Storage
	pubsub  pubsub.Client
}

func NewSystemBiz(
	storage *pgutil.Storage,
	pubsub pubsub.Client,
) (*SystemBiz, error) {
	b := &SystemBiz{
		storage: storage,
		pubsub:  pubsub,
	}

	if err := b.Init(); err != nil {
		return nil, err
	}

	return b, nil
}

func (b *SystemBiz) Init() error {
	return errutil.Some(
	//b.SetupSyncSearch(),
	)
}
