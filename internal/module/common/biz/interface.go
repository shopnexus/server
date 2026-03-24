package commonbiz

import (
	"context"
	"errors"

	"shopnexus-server/internal/infras/objectstore"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonmodel "shopnexus-server/internal/module/common/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

// CommonBiz is the interface for CommonBizImpl used by other modules.
type CommonBiz interface {
	UploadFile(ctx context.Context, params UploadFileParams) (UploadFileResult, error)
	GetFileURL(ctx context.Context, provider string, objectKey string) (string, error)
	MustGetFileURL(ctx context.Context, provider string, objectKey string) string
	UpdateServiceOptions(ctx context.Context, params UpdateServiceOptionsParams) error
	ListServiceOption(ctx context.Context, params ListServiceOptionParams) ([]sharedmodel.OptionConfig, error)
	UpdateResources(ctx context.Context, params UpdateResourcesParams) ([]commonmodel.Resource, error)
	DeleteResources(ctx context.Context, params DeleteResourcesParams) error
	GetResources(ctx context.Context, refType commondb.CommonResourceRefType, refIDs []uuid.UUID) (map[uuid.UUID][]commonmodel.Resource, error)
	GetResourcesByIDs(ctx context.Context, resourceIDs []uuid.UUID) map[uuid.UUID]commonmodel.Resource
	GetResourceURLByID(ctx context.Context, resourceID uuid.UUID) null.String
}

type CommonStorage = pgsqlc.Storage[*commondb.Queries]

// CommonBizImpl implements shared business logic used across modules.
type CommonBizImpl struct {
	storage        CommonStorage
	objectstoreMap map[string]objectstore.Client
}

// NewcommonBiz creates a new CommonBizImpl with the given dependencies.
func NewcommonBiz(storage CommonStorage) (*CommonBizImpl, error) {
	b := &CommonBizImpl{
		storage: storage,
	}

	return b, errors.Join(
		b.SetupObjectStore(),
	)
}
