package systembiz

import (
	"errors"
	systemdb "shopnexus-server/internal/module/system/db/sqlc"
	"shopnexus-server/internal/shared/pgsqlc"
)

type SystemStorage = pgsqlc.Storage[*systemdb.Queries]

// SystemHandler implements the core business logic for the system module.
type SystemHandler struct {
	storage SystemStorage
}

// NewSystemHandler creates a new SystemHandler with the given dependencies.
func NewSystemHandler(
	storage SystemStorage,
) (*SystemHandler, error) {
	b := &SystemHandler{
		storage: storage,
	}

	return b, errors.Join(
		b.SetupPubsub(),
	)
}
