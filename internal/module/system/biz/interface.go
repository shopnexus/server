package systembiz

import (
	"errors"
	"shopnexus-server/internal/infras/pubsub"
	systemdb "shopnexus-server/internal/module/system/db/sqlc"
	"shopnexus-server/internal/shared/pgsqlc"
)

type SystemStorage = pgsqlc.Storage[*systemdb.Queries]

// SystemBizHandler implements the core business logic for the system module.
type SystemBizHandler struct {
	storage SystemStorage
	pubsub  pubsub.Client
}

// NewSystemBiz creates a new SystemBizHandler with the given dependencies.
func NewSystemBiz(
	storage SystemStorage,
	pubsub pubsub.Client,
) (*SystemBizHandler, error) {
	b := &SystemBizHandler{
		storage: storage,
		pubsub:  pubsub.Group("system"),
	}

	return b, errors.Join(
		b.SetupPubsub(),
	)
}
