package sharedbiz

import (
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/sqids/sqids-go"
)

type SharedBiz struct {
	idhash  *sqids.Sqids
	storage *pgutil.Storage
}

func NewSharedBiz(storage *pgutil.Storage) *SharedBiz {
	idhash, _ := sqids.New(sqids.Options{
		MinLength: 10,
	})

	return &SharedBiz{
		idhash:  idhash,
		storage: storage,
	}
}
