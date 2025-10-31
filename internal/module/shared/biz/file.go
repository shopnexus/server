package sharedbiz

import (
	"context"
	"fmt"
	"io"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/objectstore"
	objlocal "shopnexus-remastered/internal/client/objectstore/local"
	objremote "shopnexus-remastered/internal/client/objectstore/remote"
	objs3 "shopnexus-remastered/internal/client/objectstore/s3"
	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/logger"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/google/uuid"
)

func (b *SharedBiz) SetupObjectStore() error {
	var err error
	var configs []sharedmodel.OptionConfig
	b.objectstoreMap = make(map[string]objectstore.Client)

	// setup local
	local, err := objlocal.NewClient(objlocal.LocalConfig{Root: "./uploads", BaseURL: ""})
	if err != nil {
		return err
	}
	b.objectstoreMap[local.Config().ID] = local
	configs = append(configs, local.Config())

	// setup s3
	s3, err := objs3.NewClient(objs3.S3Config{
		AccessKeyID:     config.GetConfig().Filestore.S3.AccessKeyID,
		SecretAccessKey: config.GetConfig().Filestore.S3.SecretAccessKey,
		Region:          config.GetConfig().Filestore.S3.Region,
		Bucket:          config.GetConfig().Filestore.S3.Bucket,
		CloudfrontURL:   config.GetConfig().Filestore.S3.CloudfrontURL,
	})
	if err != nil {
		return err
	}
	b.objectstoreMap[s3.Config().ID] = s3
	configs = append(configs, s3.Config())

	// setup remote
	remote := objremote.NewClient(objremote.RemoteConfig{})
	b.objectstoreMap[remote.Config().ID] = remote
	configs = append(configs, remote.Config())

	if err := b.UpdateServiceOptions(context.Background(), "objectstore", configs); err != nil {
		return err
	}

	// logger.Log.Sugar().Infof("Initialized object stores: %+v", configs)
	return nil
}

func (b *SharedBiz) mustGetObjectStore(provider string) objectstore.Client {
	client, ok := b.objectstoreMap[provider]
	if !ok {
		return b.objectstoreMap["local"]
	}
	return client
}

type UploadFileParams struct {
	Account     authmodel.AuthenticatedAccount
	File        io.Reader `validate:"required"`
	Filename    string    `validate:"required"`
	ContentType string    `validate:"required"`
	Size        int64     `validate:"required"`
	Private     bool      `validate:"omitempty"`
}

type UploadFileResult struct {
	ResourceID uuid.UUID
	Provider   string
	ObjectKey  string
	URL        string
}

// UploadFile stores a single uploaded file to the configured object store
// and creates a corresponding shared resource record.
func (b *SharedBiz) UploadFile(ctx context.Context, params UploadFileParams) (UploadFileResult, error) {
	var zero UploadFileResult

	if err := validator.Validate(params); err != nil {
		return zero, fmt.Errorf("invalid upload params: %w", err)
	}

	var err error
	var objectKey string

	myKey := fmt.Sprintf("%s_%s", uuid.New().String(), params.Filename)

	objectKey, err = b.mustGetObjectStore(config.GetConfig().Filestore.Type).Upload(ctx, myKey, params.File, params.Private)
	if err != nil {
		return zero, fmt.Errorf("upload local: %w", err)
	}

	resource, err := b.storage.CreateDefaultSharedResource(ctx, db.CreateDefaultSharedResourceParams{
		ID:         pgutil.UUIDToPgUUID(uuid.New()),
		Provider:   config.GetConfig().Filestore.Type,
		ObjectKey:  objectKey,
		UploadedBy: pgutil.Int64ToPgInt8(params.Account.ID),
		Mime:       params.ContentType,
		Size:       params.Size,
		Metadata:   []byte("{}"),
	})
	if err != nil {
		return zero, fmt.Errorf("insert resource: %w", err)
	}

	url, err := b.mustGetObjectStore(config.GetConfig().Filestore.Type).GetURL(ctx, objectKey)
	if err != nil {
		return zero, fmt.Errorf("get file url: %w", err)
	}

	return UploadFileResult{
		ResourceID: resource.ID.Bytes,
		Provider:   config.GetConfig().Filestore.Type,
		ObjectKey:  objectKey,
		URL:        url,
	}, nil
}

func (b *SharedBiz) GetFileURL(ctx context.Context, provider string, objectKey string) (string, error) {
	url, err := b.mustGetObjectStore(provider).GetURL(ctx, objectKey)
	if err != nil {
		return "", err
	}

	return url, nil
}

func (b *SharedBiz) MustGetFileURL(ctx context.Context, provider string, objectKey string) string {
	url, err := b.mustGetObjectStore(provider).GetURL(ctx, objectKey)
	if err != nil {
		// TODO: should return 404 placeholder image url
		logger.Log.Sugar().Errorf("failed to get file url for object key %s: %v", objectKey, err)
		return ""
	}

	return url
}
