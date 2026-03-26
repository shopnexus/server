package systembiz

import (
	"errors"
	"shopnexus-server/internal/infras/pubsub"
	systemdb "shopnexus-server/internal/module/system/db/sqlc"
	"shopnexus-server/internal/shared/pgsqlc"
)

type SystemStorage = pgsqlc.Storage[*systemdb.Queries]

// SystemHandler implements the core business logic for the system module.
type SystemHandler struct {
	storage SystemStorage
	pubsub  pubsub.Client
}

// NewSystemHandler creates a new SystemHandler with the given dependencies.
func NewSystemHandler(
	storage SystemStorage,
	pubsub pubsub.Client,
) (*SystemHandler, error) {
	b := &SystemHandler{
		storage: storage,
		pubsub:  pubsub.Group("system"),
	}

	return b, errors.Join(
		b.SetupPubsub(),
	)
}
