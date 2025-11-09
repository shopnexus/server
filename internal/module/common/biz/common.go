package commonbiz

import (
	"errors"
	"shopnexus-remastered/internal/infras/objectstore"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
)

type Commonbiz struct {
	storage        pgsqlc.Storage
	objectstoreMap map[string]objectstore.Client
}

func Newcommonbiz(storage pgsqlc.Storage) (*Commonbiz, error) {
	b := &Commonbiz{
		storage: storage,
	}

	return b, errors.Join(
		b.SetupObjectStore(),
	)
}
