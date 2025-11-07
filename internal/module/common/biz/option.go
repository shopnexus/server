package commonbiz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"shopnexus-remastered/internal/db"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/pgutil"
)

type UpdateServiceOptionsParams struct {
	Storage  pgsqlc.Storage
	Category string                     `validate:"required,oneof=objectstore payment shipment"`
	Configs  []commonmodel.OptionConfig `validate:"required,dive"`
}

func (b *Commonbiz) UpdateServiceOptions(ctx context.Context, params UpdateServiceOptionsParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		for index, cfg := range params.Configs {
			_, err := txStorage.GetCommonServiceOption(ctx, pgutil.StringToPgText(cfg.ID))
			if err != nil {
				// if not found, create it
				if errors.Is(err, sql.ErrNoRows) {
					_, err = txStorage.CreateCommonServiceOption(ctx, db.CreateCommonServiceOptionParams{
						ID:          cfg.ID,
						Category:    string(params.Category),
						Name:        cfg.Name,
						Description: cfg.Description,
						Provider:    cfg.Provider,
						Method:      string(cfg.Method),
						Order:       int32(index),
					})
					if err != nil {
						return err
					}
					continue
				}

				// other db error
				return err
			} else {
				// update existing
				_, err = txStorage.UpdateCommonServiceOption(ctx, db.UpdateCommonServiceOptionParams{
					ID:          cfg.ID,
					Name:        pgutil.StringToPgText(cfg.Name),
					Description: pgutil.StringToPgText(cfg.Description),
					Provider:    pgutil.StringToPgText(cfg.Provider),
					Method:      pgutil.StringToPgText(string(cfg.Method)),
					IsActive:    pgutil.BoolToPgBool(true),
					Category:    pgutil.StringToPgText(string(params.Category)),
					Order:       pgutil.Int32ToPgInt4(int32(index)),
				})
				if err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to update service options: %w", err)
	}

	return nil
}

type ListServiceOptionParams struct {
	Category []string `validate:"required"`
	IsActive []bool   `validate:"omitempty,dive"`
}

func (b *Commonbiz) ListServiceOption(ctx context.Context, params ListServiceOptionParams) ([]commonmodel.OptionConfig, error) {
	if validator.Validate(params) != nil {
		return nil, validator.Validate(params)
	}

	dbOptions, err := b.storage.SearchServiceOption(ctx, db.SearchServiceOptionParams{
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
