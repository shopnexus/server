package sharedbiz

import (
	"shopnexus-remastered/internal/client/objectstore"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgutil"
)

type SharedBiz struct {
	storage        *pgutil.Storage
	objectstoreMap map[string]objectstore.Client
}

func NewSharedBiz(storage *pgutil.Storage) (*SharedBiz, error) {
	b := &SharedBiz{
		storage: storage,
	}

	return b, errutil.Some(
		b.SetupObjectStore(),
	)
}
