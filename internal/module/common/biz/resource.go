package commonbiz

import (
	"context"
	"fmt"
	"slices"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/lo"
)

type UpdateResourcesParams struct {
	Storage         pgsqlc.Storage
	Account         authmodel.AuthenticatedAccount
	RefType         db.CommonResourceRefType `validate:"required,validateFn=Valid"`
	RefID           int64                    `validate:"required,gt=0"`
	ResourceIDs     []uuid.UUID              `validate:"omitempty,dive"` // nil with EmptyResources=true means to remove all resources , nil with EmptyResources=false means no-op
	EmptyResources  bool                     `validate:"omitempty"`
	DeleteResources bool                     `validate:"omitempty"`
}

func (b *Commonbiz) UpdateResources(ctx context.Context, params UpdateResourcesParams) ([]commonmodel.Resource, error) {
	if err := validator.Validate(params); err != nil {
		return nil, err
	}

	var resources []commonmodel.Resource

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error

		// Update resources (delete all and re-attach)
		if len(params.ResourceIDs) > 0 || params.EmptyResources {
			// First step: Delete all attached resources
			if err := b.DeleteResources(ctx, DeleteResourcesParams{
				Storage:             txStorage,
				RefType:             params.RefType,
				RefID:               []int64{params.RefID},
				DeleteResources:     params.DeleteResources,
				SkipDeleteResources: params.ResourceIDs,
			}); err != nil {
				return err
			}

			// Next step: Attach resources
			var createResourceArgs []db.CreateCopyDefaultCommonResourceReferenceParams

			resources, err := txStorage.ListCommonResource(ctx, db.ListCommonResourceParams{
				ID:         lo.Map(params.ResourceIDs, func(id uuid.UUID, _ int) pgtype.UUID { return pgutil.UUIDToPgUUID(id) }),
				UploadedBy: []pgtype.Int8{{Int64: params.Account.ID, Valid: true}}, // Can only attach own uploaded resources
			})
			if err != nil {
				return err
			}
			if len(resources) != len(params.ResourceIDs) {
				// Some resources not found or not belong to the user
				return commonmodel.ErrResourceNotFound
			}

			for order, rsID := range params.ResourceIDs {
				createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultCommonResourceReferenceParams{
					RsID:      pgutil.UUIDToPgUUID(rsID),
					RefType:   params.RefType,
					RefID:     params.RefID,
					Order:     int32(order),
					IsPrimary: false,
				})
			}

			if _, err = txStorage.CreateCopyDefaultCommonResourceReference(ctx, createResourceArgs); err != nil {
				return err
			}
		}

		resourcesMap, err := b.GetResources(ctx, params.RefType, []int64{params.RefID})
		if err != nil {
			return err
		}
		resources = resourcesMap[params.RefID]

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to update resources: %w", err)
	}

	return resources, nil
}

type DeleteResourcesParams struct {
	Storage             pgsqlc.Storage
	RefType             db.CommonResourceRefType `validate:"required,validateFn=Valid"`
	RefID               []int64                  `validate:"required,gt=0"`
	DeleteResources     bool                     `validate:"omitempty"`
	SkipDeleteResources []uuid.UUID              `validate:"omitempty,dive"` // Skip delete resource entities with these IDs (but still remove the references)
}

func (b *Commonbiz) DeleteResources(ctx context.Context, params DeleteResourcesParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		deletedResources, err := txStorage.ListCommonResourceReference(ctx, db.ListCommonResourceReferenceParams{
			RefType: []db.CommonResourceRefType{params.RefType},
			RefID:   params.RefID,
		})
		if err != nil {
			return err
		}

		deletedIDs := lo.Map(
			lo.Filter(deletedResources, func(dr db.CommonResourceReference, _ int) bool {
				return !slices.Contains(params.SkipDeleteResources, dr.RsID.Bytes) // Skip resources that should not be deleted
			}),
			func(dr db.CommonResourceReference, _ int) pgtype.UUID { return dr.RsID },
		)

		if err := txStorage.DeleteCommonResourceReference(ctx, db.DeleteCommonResourceReferenceParams{
			RefType: []db.CommonResourceRefType{params.RefType}, // just for clarity
			RsID:    deletedIDs,
		}); err != nil {
			return err
		}

		if err := txStorage.DeleteCommonResource(ctx, db.DeleteCommonResourceParams{
			ID: deletedIDs,
		}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete resources: %w", err)
	}

	return nil
}

func (b *Commonbiz) GetResources(ctx context.Context, refType db.CommonResourceRefType, refIDs []int64) (map[int64][]commonmodel.Resource, error) {
	var err error

	resources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: refType,
		RefID:   refIDs,
	})
	if err != nil {
		return nil, err
	}

	return lo.GroupByMap(resources, func(rs db.ListSortedResourcesRow) (int64, commonmodel.Resource) {
		return rs.RefID, commonmodel.Resource{
			ID:       rs.ID.Bytes,
			Url:      b.MustGetFileURL(context.Background(), rs.Provider, rs.ObjectKey),
			Mime:     rs.Mime,
			Size:     rs.Size,
			Checksum: pgutil.PgTextToNullString(rs.Checksum),
		}
	}), nil
}

func (b *Commonbiz) GetResourceURLByID(ctx context.Context, resourceID uuid.UUID) null.String {
	if resourceID == uuid.Nil {
		return null.String{}
	}

	rs, err := b.storage.GetCommonResource(ctx, db.GetCommonResourceParams{
		ID: pgutil.UUIDToPgUUID(resourceID),
	})
	if err != nil {
		return null.String{}
	}

	return null.StringFrom(b.MustGetFileURL(ctx, rs.Provider, rs.ObjectKey))
}
