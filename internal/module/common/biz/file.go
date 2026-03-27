package commonbiz

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/objectstore"
	objlocal "shopnexus-server/internal/infras/objectstore/local"
	objremote "shopnexus-server/internal/infras/objectstore/remote"
	objs3 "shopnexus-server/internal/infras/objectstore/s3"
	accountmodel "shopnexus-server/internal/module/account/model"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
)

func (b *CommonHandler) SetupObjectStore() error {
	var err error
	var configs []sharedmodel.OptionConfig
	b.objectstoreMap = make(map[string]objectstore.Client)

	// setup local
	local, err := objlocal.NewClient(objlocal.LocalConfig{Root: "./tmp/uploads", BaseURL: ""})
	if err != nil {
		return fmt.Errorf("setup local objectstore: %w", err)
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
		return fmt.Errorf("setup s3 objectstore: %w", err)
	}
	b.objectstoreMap[s3.Config().ID] = s3
	configs = append(configs, s3.Config())

	// setup remote
	remote := objremote.NewClient(objremote.RemoteConfig{})
	b.objectstoreMap[remote.Config().ID] = remote
	configs = append(configs, remote.Config())

	if err := b.updateServiceOptions(context.Background(), UpdateServiceOptionsParams{
		Category: "objectstore",
		Configs:  configs,
	}); err != nil {
		return fmt.Errorf("setup objectstore options: %w", err)
	}

	return nil
}

// getPlaceholderURL returns the configured 404 placeholder image URL, if any.
func (b *CommonHandler) getPlaceholderURL() string {
	return config.GetConfig().Filestore.Placeholder404Url
}

func (b *CommonHandler) mustGetObjectStore(provider string) objectstore.Client {
	client, ok := b.objectstoreMap[provider]
	if !ok {
		return b.objectstoreMap["local"]
	}
	return client
}

type UploadFileParams struct {
	Account     accountmodel.AuthenticatedAccount
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
// and creates a corresponding resource record.
func (b *CommonHandler) UploadFile(ctx restate.Context, params UploadFileParams) (UploadFileResult, error) {
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

	resource, err := b.storage.Querier().CreateDefaultResource(ctx, commondb.CreateDefaultResourceParams{
		Provider:   config.GetConfig().Filestore.Type,
		ObjectKey:  objectKey,
		UploadedBy: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
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
		ResourceID: resource.ID,
		Provider:   config.GetConfig().Filestore.Type,
		ObjectKey:  objectKey,
		URL:        url,
	}, nil
}

type GetFileURLParams struct {
	Provider  string
	ObjectKey string
}

func (b *CommonHandler) GetFileURL(ctx restate.Context, params GetFileURLParams) (string, error) {
	url, err := b.mustGetObjectStore(params.Provider).GetURL(ctx, params.ObjectKey)
	if err != nil {
		return "", fmt.Errorf("get file url: %w", err)
	}

	return url, nil
}

// mustGetFileURL returns the URL for an object key, falling back to a placeholder on error.
func (b *CommonHandler) mustGetFileURL(ctx context.Context, provider string, objectKey string) string {
	url, err := b.mustGetObjectStore(provider).GetURL(ctx, objectKey)
	if err != nil {
		slog.Error("failed to get file url for object key", slog.String("object_key", objectKey), slog.String("provider", provider), slog.Any("error", err))
		return b.getPlaceholderURL()
	}

	return url
}
