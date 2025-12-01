package commonbiz

import (
	"context"
	"fmt"
	"slices"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	commondb "shopnexus-remastered/internal/module/common/db"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/shared/pgsqlc"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type UpdateResourcesParams struct {
	Storage         pgsqlc.Storage[*commondb.Queries]
	Account         accountmodel.AuthenticatedAccount
	RefType         commondb.CommonResourceRefType `validate:"required,validateFn=Valid"`
	RefID           uuid.UUID                      `validate:"required,gt=0"`
	ResourceIDs     []uuid.UUID                    `validate:"omitempty,dive"` // nil with EmptyResources=true means to remove all resources , nil with EmptyResources=false means no-op
	EmptyResources  bool                           `validate:"omitempty"`
	DeleteResources bool                           `validate:"omitempty"`
}

func (b *CommonBiz) UpdateResources(ctx context.Context, params UpdateResourcesParams) ([]commonmodel.Resource, error) {
	if err := validator.Validate(params); err != nil {
		return nil, err
	}

	var resources []commonmodel.Resource

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage[*commondb.Queries]) error {
		var err error

		// Update resources (delete all and re-attach)
		if len(params.ResourceIDs) > 0 || params.EmptyResources {
			// First step: Delete all attached resources
			if err := b.DeleteResources(ctx, DeleteResourcesParams{
				Storage:             txStorage,
				RefType:             params.RefType,
				RefID:               []uuid.UUID{params.RefID},
				DeleteResources:     params.DeleteResources,
				SkipDeleteResources: params.ResourceIDs,
			}); err != nil {
				return err
			}

			// Next step: Attach resources
			var createResourceArgs []commondb.CreateCopyDefaultResourceReferenceParams

			resources, err := txStorage.Querier().ListResource(ctx, commondb.ListResourceParams{
				ID: params.ResourceIDs,
			})
			if err != nil {
				return err
			}
			if len(resources) != len(params.ResourceIDs) {
				// Some resources not found or not belong to the user
				return commonmodel.ErrResourceNotFound
			}

			for order, rsID := range params.ResourceIDs {
				createResourceArgs = append(createResourceArgs, commondb.CreateCopyDefaultResourceReferenceParams{
					RsID:    rsID,
					RefType: params.RefType,
					RefID:   params.RefID,
					Order:   int32(order),
				})
			}

			if _, err = txStorage.Querier().CreateCopyDefaultResourceReference(ctx, createResourceArgs); err != nil {
				return err
			}
		}

		resourcesMap, err := b.GetResources(ctx, params.RefType, []uuid.UUID{params.RefID})
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
	Storage             pgsqlc.Storage[*commondb.Queries]
	RefType             commondb.CommonResourceRefType `validate:"required,validateFn=Valid"`
	RefID               []uuid.UUID                    `validate:"required,dive"`
	DeleteResources     bool                           `validate:"omitempty"`
	SkipDeleteResources []uuid.UUID                    `validate:"omitempty,dive"` // Skip delete resource entities with these IDs (but still remove the references)
}

func (b *CommonBiz) DeleteResources(ctx context.Context, params DeleteResourcesParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage[*commondb.Queries]) error {
		deletedResources, err := txStorage.Querier().ListResourceReference(ctx, commondb.ListResourceReferenceParams{
			RefType: []commondb.CommonResourceRefType{params.RefType},
			RefID:   params.RefID,
		})
		if err != nil {
			return err
		}

		var deletedIDs []uuid.UUID
		for _, dr := range deletedResources {
			// Skip deleting resource entity if in skip list
			if !slices.Contains(params.SkipDeleteResources, dr.RsID) {
				deletedIDs = append(deletedIDs, dr.RsID)
			}
		}

		if err := txStorage.Querier().DeleteResourceReference(ctx, commondb.DeleteResourceReferenceParams{
			RefType: []commondb.CommonResourceRefType{params.RefType}, // just for clarity
			RsID:    deletedIDs,
		}); err != nil {
			return err
		}

		if err := txStorage.Querier().DeleteResource(ctx, commondb.DeleteResourceParams{
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

func (b *CommonBiz) GetResources(ctx context.Context, refType commondb.CommonResourceRefType, refIDs []uuid.UUID) (map[uuid.UUID][]commonmodel.Resource, error) {
	var err error

	resources, err := b.storage.Querier().ListSortedResources(ctx, commondb.ListSortedResourcesParams{
		RefType: refType,
		RefID:   refIDs,
	})
	if err != nil {
		return nil, err
	}

	return lo.GroupByMap(resources, func(rs commondb.ListSortedResourcesRow) (uuid.UUID, commonmodel.Resource) {
		return rs.RefID, commonmodel.Resource{
			ID:       rs.ID,
			Url:      b.MustGetFileURL(context.Background(), rs.Provider, rs.ObjectKey),
			Mime:     rs.Mime,
			Size:     rs.Size,
			Checksum: rs.Checksum,
		}
	}), nil
}

func (b *CommonBiz) GetResourcesByIDs(ctx context.Context, resourceIDs []uuid.UUID) map[uuid.UUID]commonmodel.Resource {
	result := make(map[uuid.UUID]commonmodel.Resource)
	for _, rsID := range resourceIDs {
		result[rsID] = commonmodel.Resource{
			Url: "", // TODO: use 404 placeholder image URL
		}
	}

	resources, err := b.storage.Querier().ListResource(ctx, commondb.ListResourceParams{
		ID: resourceIDs,
	})
	if err != nil {
		return result
	}

	for _, rs := range resources {
		result[rs.ID] = commonmodel.Resource{
			ID:       rs.ID,
			Url:      b.MustGetFileURL(context.Background(), rs.Provider, rs.ObjectKey),
			Mime:     rs.Mime,
			Size:     rs.Size,
			Checksum: rs.Checksum,
		}
	}

	return result
}

func (b *CommonBiz) GetResourceURLByID(ctx context.Context, resourceID uuid.UUID) null.String {
	resource, err := b.storage.Querier().GetResource(ctx, commondb.GetResourceParams{
		ID: uuid.NullUUID{UUID: resourceID, Valid: true},
	})
	if err != nil {
		return null.String{}
	}

	url, err := b.mustGetObjectStore(resource.Provider).GetURL(ctx, resource.ObjectKey)
	if err != nil {
		return null.String{}
	}

	return null.StringFrom(url)
}
