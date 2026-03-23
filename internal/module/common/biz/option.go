package commonbiz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/guregu/null/v6"
)

type UpdateServiceOptionsParams struct {
	Category string                     `validate:"required,oneof=objectstore payment shipment"`
	Configs  []commonmodel.OptionConfig `validate:"required,dive"`
}

func (b *CommonBiz) UpdateServiceOptions(ctx context.Context, params UpdateServiceOptionsParams) error {
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
					return fmt.Errorf("failed to update service options: %w", err)
				}
				continue
			}

			// other db error
			return fmt.Errorf("failed to update service options: %w", err)
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
				return fmt.Errorf("failed to update service options: %w", err)
			}
		}
	}

	return nil
}

type ListServiceOptionParams struct {
	Category []string `validate:"required"`
	IsActive []bool   `validate:"omitempty,dive"`
}

func (b *CommonBiz) ListServiceOption(ctx context.Context, params ListServiceOptionParams) ([]commonmodel.OptionConfig, error) {
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

	var result []commonmodel.OptionConfig
	for _, dbOpt := range dbOptions {
		opt := commonmodel.OptionConfig{
			ID:          dbOpt.ID,
			Name:        dbOpt.Name,
			Description: dbOpt.Description,
			Provider:    dbOpt.Provider,
			Method:      commonmodel.OptionMethod(dbOpt.Method),
		}
		result = append(result, opt)
	}

	return result, nil
}
