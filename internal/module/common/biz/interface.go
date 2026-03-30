package commonbiz

import (
	"context"
	"errors"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/objectstore"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonmodel "shopnexus-server/internal/module/common/model"
	"shopnexus-server/internal/provider/geocoding"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

// CommonBiz is the interface for common module, used by other modules.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface CommonBiz -service Common
type CommonBiz interface {
	// File
	GetFileURL(ctx context.Context, params GetFileURLParams) (string, error)

	// Option
	UpdateServiceOptions(ctx context.Context, params UpdateServiceOptionsParams) error
	ListServiceOption(ctx context.Context, params ListServiceOptionParams) ([]sharedmodel.OptionConfig, error)

	// Resource
	UpdateResources(ctx context.Context, params UpdateResourcesParams) ([]commonmodel.Resource, error)
	DeleteResources(ctx context.Context, params DeleteResourcesParams) error
	GetResources(ctx context.Context, params GetResourcesParams) (map[uuid.UUID][]commonmodel.Resource, error)
	GetResourcesByIDs(ctx context.Context, resourceIDs []uuid.UUID) (map[uuid.UUID]commonmodel.Resource, error)
	GetResourceByID(ctx context.Context, resourceID uuid.UUID) (*commonmodel.Resource, error)

	// Geocoding
	ReverseGeocode(ctx context.Context, params ReverseGeocodeParams) (geocoding.Result, error)
	ForwardGeocode(ctx context.Context, params ForwardGeocodeParams) (geocoding.Result, error)
	SearchGeocode(ctx context.Context, params SearchGeocodeParams) ([]geocoding.Result, error)
}

type CommonStorage = pgsqlc.Storage[*commondb.Queries]

// CommonHandler implements shared business logic used across modules.
type CommonHandler struct {
	config         *config.Config
	storage        CommonStorage
	objectstoreMap map[string]objectstore.Client
	geocoder       geocoding.Client
}

func (b *CommonHandler) ServiceName() string {
	return "Common"
}

// NewcommonBiz creates a new CommonHandler with the given dependencies.
func NewcommonBiz(cfg *config.Config, storage CommonStorage, geocoder geocoding.Client) (*CommonHandler, error) {
	b := &CommonHandler{
		config:   cfg,
		storage:  storage,
		geocoder: geocoder,
	}

	return b, errors.Join(
		b.SetupObjectStore(),
	)
}
