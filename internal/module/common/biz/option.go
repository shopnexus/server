package commonbiz

import (
	"context"

	restate "github.com/restatedev/sdk-go"

	commondb "shopnexus-server/internal/module/common/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

type ListOptionParams struct {
	Type      []string `validate:"required"`
	IsEnabled []bool   `validate:"omitempty,dive"`
}

type UpsertOptionsParams struct {
	Category string               `json:"category" validate:"required"`
	Configs  []sharedmodel.Option `json:"configs"  validate:"required"`
}

// UpsertOptions persists a batch of service options (insert or update by ID).
func (b *CommonHandler) UpsertOptions(ctx restate.Context, params UpsertOptionsParams) error {
	return b.upsertOptions(ctx, params)
}

// upsertOptions is the context-agnostic implementation, used at init time
// where we hold a plain context.Context (not a Restate one).
func (b *CommonHandler) upsertOptions(ctx context.Context, params UpsertOptionsParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate upsert options", err)
	}

	q := b.storage.Querier()
	for _, cfg := range params.Configs {
		if err := q.UpsertOption(ctx, commondb.UpsertOptionParams{
			ID:          cfg.ID,
			OwnerID:     cfg.OwnerID,
			IsEnabled:   true,
			Name:        cfg.Name,
			Description: cfg.Description,
			Priority:    cfg.Priority,
			LogoRsID:    cfg.LogoRsID,
			Data:        cfg.Data,
			Type:        string(cfg.Type),
			Provider:    cfg.Provider,
		}); err != nil {
			return sharedmodel.WrapErr("db upsert option", err)
		}
	}
	return nil
}

// ListOption returns active service options filtered by category.
func (b *CommonHandler) ListOption(
	ctx restate.Context,
	params ListOptionParams,
) ([]sharedmodel.Option, error) {
	if err := validator.Validate(params); err != nil {
		return nil, sharedmodel.WrapErr("validate list service option", err)
	}

	dbOptions, err := b.storage.Querier().ListSortedOption(ctx, commondb.ListSortedOptionParams{
		Type:      params.Type,
		IsEnabled: params.IsEnabled,
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db list service option", err)
	}

	var result []sharedmodel.Option
	for _, opts := range dbOptions {
		result = append(result, sharedmodel.Option{
			ID:          opts.ID,
			OwnerID:     opts.OwnerID,
			Type:        sharedmodel.OptionType(opts.Type),
			IsEnabled:   opts.IsEnabled,
			Provider:    opts.Provider,
			Name:        opts.Name,
			Description: opts.Description,
			Priority:    opts.Priority,
			LogoRsID:    opts.LogoRsID,
			Data:        opts.Data,
		})
	}

	return result, nil
}
