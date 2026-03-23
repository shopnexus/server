package commonbiz

import (
	"errors"
	"shopnexus-server/internal/infras/objectstore"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	"shopnexus-server/internal/shared/pgsqlc"
)

type CommonStorage = pgsqlc.Storage[*commondb.Queries]

type CommonBiz struct {
	storage        CommonStorage
	objectstoreMap map[string]objectstore.Client
}

func NewcommonBiz(storage CommonStorage) (*CommonBiz, error) {
	b := &CommonBiz{
		storage: storage,
	}

	return b, errors.Join(
		b.SetupObjectStore(),
	)
}
