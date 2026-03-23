package commonbiz

import (
	"errors"
	"shopnexus-server/internal/infras/objectstore"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	"shopnexus-server/internal/shared/pgsqlc"
)

type CommonStorage = pgsqlc.Storage[*commondb.Queries]

// CommonBiz implements shared business logic used across modules.
type CommonBiz struct {
	storage        CommonStorage
	objectstoreMap map[string]objectstore.Client
}

// NewcommonBiz creates a new CommonBiz with the given dependencies.
func NewcommonBiz(storage CommonStorage) (*CommonBiz, error) {
	b := &CommonBiz{
		storage: storage,
	}

	return b, errors.Join(
		b.SetupObjectStore(),
	)
}
