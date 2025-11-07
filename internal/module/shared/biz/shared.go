package sharedbiz

import (
	"context"
	"shopnexus-remastered/internal/client/objectstore"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type SharedBiz struct {
	storage        *pgutil.Storage
	objectstoreMap map[string]objectstore.Client
}

func NewSharedBiz(storage *pgutil.Storage) (*SharedBiz, error) {
	b := &SharedBiz{
		storage: storage,
	}

	return b, errutil.Some(
		b.SetupObjectStore(),
	)
}

type UpdateResourcesParams struct {
	Account         authmodel.AuthenticatedAccount
	RefType         db.SharedResourceRefType `validate:"required,validateFn=Valid"`
	RefID           int64                    `validate:"required,gt=0"`
	ResourceIDs     []uuid.UUID              `validate:"omitempty,dive"` // nil with EmptyResources=true means to remove all resources , nil with EmptyResources=false means no-op
	EmptyResources  bool                     `validate:"omitempty"`
	DeleteResources bool                     `validate:"omitempty"`
}

func (b *SharedBiz) UpdateResources(ctx context.Context, txStorage *pgsqlc.Storage, params UpdateResourcesParams) ([]sharedmodel.Resource, error) {
	if err := validator.Validate(params); err != nil {
		return nil, err
	}

	// Update resources (delete all and re-attach)
	if len(params.ResourceIDs) > 0 || params.EmptyResources {
		// First step: Delete all attached resources
		if err := b.DeleteResources(ctx, txStorage, DeleteResourcesParams{
			RefType:             params.RefType,
			RefID:               []int64{params.RefID},
			DeleteResources:     params.DeleteResources,
			SkipDeleteResources: params.ResourceIDs,
		}); err != nil {
			return nil, err
		}

		// Next step: Attach resources
		var createResourceArgs []db.CreateCopyDefaultSharedResourceReferenceParams

		resources, err := txStorage.ListSharedResource(ctx, db.ListSharedResourceParams{
			ID:         slice.Map(params.ResourceIDs, func(id uuid.UUID) pgtype.UUID { return pgutil.UUIDToPgUUID(id) }),
			UploadedBy: []pgtype.Int8{{Int64: params.Account.ID, Valid: true}}, // Can only attach own uploaded resources
		})
		if err != nil {
			return nil, err
		}
		if len(resources) != len(params.ResourceIDs) {
			// Some resources not found or not belong to the user
			return nil, sharedmodel.ErrResourceNotFound
		}

		for order, rsID := range params.ResourceIDs {
			createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceReferenceParams{
				RsID:      pgutil.UUIDToPgUUID(rsID),
				RefType:   params.RefType,
				RefID:     params.RefID,
				Order:     int32(order),
				IsPrimary: false,
			})
		}

		if _, err = txStorage.CreateCopyDefaultSharedResourceReference(ctx, createResourceArgs); err != nil {
			return nil, err
		}
	}

	resourcesMap, err := b.GetResources(ctx, params.RefType, []int64{params.RefID})
	if err != nil {
		return nil, err
	}

	return resourcesMap[params.RefID], nil
}

type DeleteResourcesParams struct {
	RefType             db.SharedResourceRefType `validate:"required,validateFn=Valid"`
	RefID               []int64                  `validate:"required,gt=0"`
	DeleteResources     bool                     `validate:"omitempty"`
	SkipDeleteResources []uuid.UUID              `validate:"omitempty,dive"` // Skip delete resource entities with these IDs (but still remove the references)
}

func (b *SharedBiz) DeleteResources(ctx context.Context, txStorage *pgsqlc.Storage, params DeleteResourcesParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	deletedResources, err := txStorage.ListSharedResourceReference(ctx, db.ListSharedResourceReferenceParams{
		RefType: []db.SharedResourceRefType{params.RefType},
		RefID:   params.RefID,
	})
	if err != nil {
		return err
	}

	deletedIDs := slice.Map(
		slice.Filter(deletedResources, func(dr db.SharedResourceReference) bool {
			return !slices.Contains(params.SkipDeleteResources, dr.RsID.Bytes) // Skip resources that should not be deleted
		}),
		func(dr db.SharedResourceReference) pgtype.UUID { return dr.RsID },
	)

	if err := txStorage.DeleteSharedResourceReference(ctx, db.DeleteSharedResourceReferenceParams{
		RefType: []db.SharedResourceRefType{params.RefType}, // just for clarity
		RsID:    deletedIDs,
	}); err != nil {
		return err
	}

	if err := txStorage.DeleteSharedResource(ctx, db.DeleteSharedResourceParams{
		ID: deletedIDs,
	}); err != nil {
		return err
	}

	return nil
}

func (b *SharedBiz) GetResources(ctx context.Context, refType db.SharedResourceRefType, refIDs []int64) (map[int64][]sharedmodel.Resource, error) {
	var err error

	resources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: refType,
		RefID:   refIDs,
	})
	if err != nil {
		return nil, err
	}

	return slice.GroupBySlice(resources, func(rs db.ListSortedResourcesRow) (int64, sharedmodel.Resource) {
		return rs.RefID, sharedmodel.Resource{
			ID:       rs.ID.Bytes,
			Url:      b.MustGetFileURL(context.Background(), rs.Provider, rs.ObjectKey),
			Mime:     rs.Mime,
			Size:     rs.Size,
			Checksum: pgutil.PgTextToNullString(rs.Checksum),
		}
	}), nil
}
