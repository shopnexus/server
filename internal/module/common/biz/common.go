package commonbiz

import (
	"shopnexus-remastered/internal/client/objectstore"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgsqlc"
)

type Commonbiz struct {
	storage        pgsqlc.Storage
	objectstoreMap map[string]objectstore.Client
}

func Newcommonbiz(storage pgsqlc.Storage) (*Commonbiz, error) {
	b := &Commonbiz{
		storage: storage,
	}

	return b, errutil.Some(
		b.SetupObjectStore(),
	)
}
