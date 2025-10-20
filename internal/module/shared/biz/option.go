package sharedbiz

import (
	"context"
	"database/sql"
	"errors"
	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/utils/pgutil"
)

func (b *SharedBiz) UpdateServiceOptions(ctx context.Context, category string, configs []sharedmodel.OptionConfig) error {
	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	for _, cfg := range configs {
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
					IsActive:    cfg.IsActive,
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
				IsActive:    pgutil.BoolToPgBool(cfg.IsActive),
				Category:    pgutil.StringToPgText(category),
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
