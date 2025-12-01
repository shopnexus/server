package commonbiz

import (
	"errors"
	"shopnexus-remastered/internal/infras/objectstore"
	commondb "shopnexus-remastered/internal/module/common/db"
	"shopnexus-remastered/internal/shared/pgsqlc"
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
