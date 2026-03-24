package commonbiz

import (
	"context"
	"fmt"
	"slices"

	accountmodel "shopnexus-server/internal/module/account/model"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonmodel "shopnexus-server/internal/module/common/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type UpdateResourcesParams struct {
	Account         accountmodel.AuthenticatedAccount
	RefType         commondb.CommonResourceRefType `validate:"required,validateFn=Valid"`
	RefID           uuid.UUID                      `validate:"required,gt=0"`
	ResourceIDs     []uuid.UUID                    `validate:"omitempty,dive"` // nil with EmptyResources=true means to remove all resources , nil with EmptyResources=false means no-op
	EmptyResources  bool                           `validate:"omitempty"`
	DeleteResources bool                           `validate:"omitempty"`
}

// UpdateResources replaces all resource references for a given entity and returns the updated list.
func (b *CommonBizImpl) UpdateResources(ctx context.Context, params UpdateResourcesParams) ([]commonmodel.Resource, error) {
	if err := validator.Validate(params); err != nil {
		return nil, err
	}

	// Update resources (delete all and re-attach)
	if len(params.ResourceIDs) > 0 || params.EmptyResources {
		// First step: Delete all attached resources
		if err := b.DeleteResources(ctx, DeleteResourcesParams{
			RefType:             params.RefType,
			RefID:               []uuid.UUID{params.RefID},
			DeleteResources:     params.DeleteResources,
			SkipDeleteResources: params.ResourceIDs,
		}); err != nil {
			return nil, fmt.Errorf("update resources: %w", err)
		}

		// Next step: Attach resources
		var createResourceArgs []commondb.CreateCopyDefaultResourceReferenceParams

		resources, err := b.storage.Querier().ListResource(ctx, commondb.ListResourceParams{
			ID: params.ResourceIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("update resources: %w", err)
		}
		if len(resources) != len(params.ResourceIDs) {
			// Some resources not found or not belong to the user
			return nil, commonmodel.ErrResourceNotFound.Terminal()
		}

		for order, rsID := range params.ResourceIDs {
			createResourceArgs = append(createResourceArgs, commondb.CreateCopyDefaultResourceReferenceParams{
				RsID:    rsID,
				RefType: params.RefType,
				RefID:   params.RefID,
				Order:   int32(order),
			})
		}

		if _, err = b.storage.Querier().CreateCopyDefaultResourceReference(ctx, createResourceArgs); err != nil {
			return nil, fmt.Errorf("update resources: %w", err)
		}
	}

	resourcesMap, err := b.GetResources(ctx, params.RefType, []uuid.UUID{params.RefID})
	if err != nil {
		return nil, fmt.Errorf("update resources: %w", err)
	}

	return resourcesMap[params.RefID], nil
}

type DeleteResourcesParams struct {
	RefType             commondb.CommonResourceRefType `validate:"required,validateFn=Valid"`
	RefID               []uuid.UUID                    `validate:"required,dive"`
	DeleteResources     bool                           `validate:"omitempty"`
	SkipDeleteResources []uuid.UUID                    `validate:"omitempty,dive"` // Skip delete resource entities with these IDs (but still remove the references)
}

// DeleteResources removes resource references and optionally deletes the underlying resource records.
func (b *CommonBizImpl) DeleteResources(ctx context.Context, params DeleteResourcesParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	deletedResources, err := b.storage.Querier().ListResourceReference(ctx, commondb.ListResourceReferenceParams{
		RefType: []commondb.CommonResourceRefType{params.RefType},
		RefID:   params.RefID,
	})
	if err != nil {
		return fmt.Errorf("delete resources: %w", err)
	}

	var deletedIDs []uuid.UUID
	for _, dr := range deletedResources {
		// Skip deleting resource entity if in skip list
		if !slices.Contains(params.SkipDeleteResources, dr.RsID) {
			deletedIDs = append(deletedIDs, dr.RsID)
		}
	}

	if err := b.storage.Querier().DeleteResourceReference(ctx, commondb.DeleteResourceReferenceParams{
		RefType: []commondb.CommonResourceRefType{params.RefType}, // just for clarity
		RsID:    deletedIDs,
	}); err != nil {
		return fmt.Errorf("delete resources: %w", err)
	}

	if params.DeleteResources {
		if err := b.storage.Querier().DeleteResource(ctx, commondb.DeleteResourceParams{
			ID: deletedIDs,
		}); err != nil {
			return fmt.Errorf("delete resources: %w", err)
		}
	}

	return nil
}

// GetResources returns resources grouped by reference ID for the given ref type and IDs.
func (b *CommonBizImpl) GetResources(ctx context.Context, refType commondb.CommonResourceRefType, refIDs []uuid.UUID) (map[uuid.UUID][]commonmodel.Resource, error) {
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

// GetResourcesByIDs returns a map of resources keyed by their IDs, falling back to placeholder URLs on error.
func (b *CommonBizImpl) GetResourcesByIDs(ctx context.Context, resourceIDs []uuid.UUID) map[uuid.UUID]commonmodel.Resource {
	result := make(map[uuid.UUID]commonmodel.Resource)
	for _, rsID := range resourceIDs {
		result[rsID] = commonmodel.Resource{
			Url: b.getPlaceholderURL(),
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

func (b *CommonBizImpl) GetResourceURLByID(ctx context.Context, resourceID uuid.UUID) null.String {
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
