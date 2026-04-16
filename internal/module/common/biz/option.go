package commonbiz

import (
	"context"
	"database/sql"
	"errors"

	"github.com/guregu/null/v6"
	restate "github.com/restatedev/sdk-go"

	commondb "shopnexus-server/internal/module/common/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

type UpdateServiceOptionsParams struct {
	Category string                     `validate:"required,oneof=objectstore payment transport"`
	Configs  []sharedmodel.OptionConfig `validate:"required,dive"`
}

// UpdateServiceOptions creates or updates service option configurations for a given category.
func (b *CommonHandler) UpdateServiceOptions(ctx restate.Context, params UpdateServiceOptionsParams) error {
	return b.updateServiceOptions(ctx, params)
}

// updateServiceOptions is an internal helper that accepts context.Context,
// used by both UpdateServiceOptions (restate.Context) and SetupObjectStore (context.Background()).
func (b *CommonHandler) updateServiceOptions(ctx context.Context, params UpdateServiceOptionsParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate service options", err)
	}

	for index, cfg := range params.Configs {
		config := cfg.Config
		if config == nil {
			config = []byte("{}")
		}

		_, err := b.storage.Querier().GetServiceOption(ctx, null.StringFrom(cfg.ID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_, err = b.storage.Querier().CreateServiceOption(ctx, commondb.CreateServiceOptionParams{
					ID:          cfg.ID,
					Category:    params.Category,
					Provider:    cfg.Provider,
					IsActive:    true,
					Name:        cfg.Name,
					Description: cfg.Description,
					Priority:    int32(index),
					Config:      config,
					LogoRsID:    cfg.LogoRsID,
				})
				if err != nil {
					return sharedmodel.WrapErr("db create service option", err)
				}
				continue
			}

			return sharedmodel.WrapErr("db get service option", err)
		}

		_, err = b.storage.Querier().UpdateServiceOption(ctx, commondb.UpdateServiceOptionParams{
			ID:          cfg.ID,
			Category:    null.StringFrom(params.Category),
			Provider:    null.StringFrom(cfg.Provider),
			IsActive:    null.BoolFrom(true),
			Name:        null.StringFrom(cfg.Name),
			Description: null.StringFrom(cfg.Description),
			Priority:    null.Int32From(int32(index)),
			Config:      config,
			LogoRsID:    cfg.LogoRsID,
		})
		if err != nil {
			return sharedmodel.WrapErr("db update service option", err)
		}
	}

	return nil
}

type ListServiceOptionParams struct {
	Category []string `validate:"required"`
	IsActive []bool   `validate:"omitempty,dive"`
}

// ListServiceOption returns active service options filtered by category.
func (b *CommonHandler) ListServiceOption(
	ctx restate.Context,
	params ListServiceOptionParams,
) ([]sharedmodel.OptionConfig, error) {
	if err := validator.Validate(params); err != nil {
		return nil, sharedmodel.WrapErr("validate list service option", err)
	}

	dbOptions, err := b.storage.Querier().ListSortedServiceOption(ctx, commondb.ListSortedServiceOptionParams{
		Category: params.Category,
		IsActive: params.IsActive,
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db list service option", err)
	}

	var result []sharedmodel.OptionConfig
	for _, dbOpt := range dbOptions {
		result = append(result, sharedmodel.OptionConfig{
			ID:          dbOpt.ID,
			Provider:    dbOpt.Provider,
			Name:        dbOpt.Name,
			Description: dbOpt.Description,
			Priority:    dbOpt.Priority,
			Config:      dbOpt.Config,
			LogoRsID:    dbOpt.LogoRsID,
		})
	}

	return result, nil
}
