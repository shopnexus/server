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

// CommonBiz is the interface for common module, used by other modules.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface CommonBiz -service CommonBiz
type CommonBiz interface {
	// File
	UploadFile(ctx context.Context, params UploadFileParams) (UploadFileResult, error)
	GetFileURL(ctx context.Context, params GetFileURLParams) (string, error)

	// Option
	UpdateServiceOptions(ctx context.Context, params UpdateServiceOptionsParams) error
	ListServiceOption(ctx context.Context, params ListServiceOptionParams) ([]sharedmodel.OptionConfig, error)

	// Resource
	UpdateResources(ctx context.Context, params UpdateResourcesParams) ([]commonmodel.Resource, error)
	DeleteResources(ctx context.Context, params DeleteResourcesParams) error
	GetResources(ctx context.Context, params GetResourcesParams) (map[uuid.UUID][]commonmodel.Resource, error)
	GetResourcesByIDs(ctx context.Context, resourceIDs []uuid.UUID) (map[uuid.UUID]commonmodel.Resource, error)
	GetResourceURLByID(ctx context.Context, resourceID uuid.UUID) (null.String, error)
}

// Param structs for multi-param methods

type GetFileURLParams struct {
	Provider  string
	ObjectKey string
}

type GetResourcesParams struct {
	RefType commondb.CommonResourceRefType
	RefIDs  []uuid.UUID
}

type CommonStorage = pgsqlc.Storage[*commondb.Queries]

// CommonBizHandler implements shared business logic used across modules.
type CommonBizHandler struct {
	storage        CommonStorage
	objectstoreMap map[string]objectstore.Client
}

// NewcommonBiz creates a new CommonBizHandler with the given dependencies.
func NewcommonBiz(storage CommonStorage) (*CommonBizHandler, error) {
	b := &CommonBizHandler{
		storage: storage,
	}

	return b, errors.Join(
		b.SetupObjectStore(),
	)
}
