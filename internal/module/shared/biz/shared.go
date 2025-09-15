package biz

import (
	"context"
	"mime/multipart"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
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

type UploadFileParams struct {
	Files []multipart.File
}

func (b *SharedBiz) UploadFile(ctx context.Context, params UploadFileParams) ([]sharedmodel.Resource, error) {

	return nil, nil
}
