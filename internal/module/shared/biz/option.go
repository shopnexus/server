package sharedbiz

import (
	"context"
	"database/sql"
	"errors"
	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
)

func (b *SharedBiz) UpdateServiceOptions(ctx context.Context, category string, configs []sharedmodel.OptionConfig) error {
	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	for index, cfg := range configs {
		_, err := txStorage.GetSharedServiceOption(ctx, pgutil.StringToPgText(cfg.ID))
		if err != nil {
			// if not found, create it
			if errors.Is(err, sql.ErrNoRows) {
				_, err = txStorage.CreateSharedServiceOption(ctx, db.CreateSharedServiceOptionParams{
					ID:          cfg.ID,
					Category:    category,
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
			_, err = txStorage.UpdateSharedServiceOption(ctx, db.UpdateSharedServiceOptionParams{
				ID:          cfg.ID,
				Name:        pgutil.StringToPgText(cfg.Name),
				Description: pgutil.StringToPgText(cfg.Description),
				Provider:    pgutil.StringToPgText(cfg.Provider),
				Method:      pgutil.StringToPgText(string(cfg.Method)),
				IsActive:    pgutil.BoolToPgBool(true),
				Category:    pgutil.StringToPgText(category),
				Order:       pgutil.Int32ToPgInt4(int32(index)),
			})
			if err != nil {
				return err
			}
		}
	}

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}

type ListServiceOptionParams struct {
	Category []string `validate:"required"`
	IsActive []bool   `validate:"omitempty,dive"`
}

func (b *SharedBiz) ListServiceOption(ctx context.Context, params ListServiceOptionParams) ([]sharedmodel.OptionConfig, error) {
	if validator.Validate(params) != nil {
		return nil, validator.Validate(params)
	}

	dbOptions, err := b.storage.SearchSharedServiceOption(ctx, db.SearchSharedServiceOptionParams{
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
