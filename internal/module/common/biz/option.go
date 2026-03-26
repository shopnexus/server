package commonbiz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	commondb "shopnexus-server/internal/module/common/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/guregu/null/v6"
)

type UpdateServiceOptionsParams struct {
	Category string                     `validate:"required,oneof=objectstore payment shipment"`
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
		return err
	}

	for index, cfg := range params.Configs {
		_, err := b.storage.Querier().GetServiceOption(ctx, null.StringFrom(cfg.ID))
		if err != nil {
			// if not found, create it
			if errors.Is(err, sql.ErrNoRows) {
				_, err = b.storage.Querier().CreateServiceOption(ctx, commondb.CreateServiceOptionParams{
					ID:          cfg.ID,
					Category:    string(params.Category),
					Name:        cfg.Name,
					Description: cfg.Description,
					Provider:    cfg.Provider,
					Method:      string(cfg.Method),
					Order:       int32(index),
				})
				if err != nil {
					return fmt.Errorf("update service options: %w", err)
				}
				continue
			}

			// other db error
			return fmt.Errorf("update service options: %w", err)
		} else {
			// update existing
			_, err = b.storage.Querier().UpdateServiceOption(ctx, commondb.UpdateServiceOptionParams{
				ID:          cfg.ID,
				Name:        null.StringFrom(cfg.Name),
				Description: null.StringFrom(cfg.Description),
				Provider:    null.StringFrom(cfg.Provider),
				Method:      null.StringFrom(string(cfg.Method)),
				IsActive:    null.BoolFrom(true),
				Category:    null.StringFrom(string(params.Category)),
				Order:       null.Int32From(int32(index)),
			})
			if err != nil {
				return fmt.Errorf("update service options: %w", err)
			}
		}
	}

	return nil
}

type ListServiceOptionParams struct {
	Category []string `validate:"required"`
	IsActive []bool   `validate:"omitempty,dive"`
}

// ListServiceOption returns active service options filtered by category.
func (b *CommonHandler) ListServiceOption(ctx restate.Context, params ListServiceOptionParams) ([]sharedmodel.OptionConfig, error) {
	if validator.Validate(params) != nil {
		return nil, validator.Validate(params)
	}

	dbOptions, err := b.storage.Querier().ListSortedServiceOption(ctx, commondb.ListSortedServiceOptionParams{
		Category: params.Category,
		IsActive: params.IsActive,
	})
	if err != nil {
		return nil, err
	}

	var result []sharedmodel.OptionConfig
	for _, dbOpt := range dbOptions {
		opt := sharedmodel.OptionConfig{
			ID:          dbOpt.ID,
			Name:        dbOpt.Name,
			Description: dbOpt.Description,
			Provider:    dbOpt.Provider,
			Method:      sharedmodel.OptionMethod(dbOpt.Method),
		}
		result = append(result, opt)
	}

	return result, nil
}
