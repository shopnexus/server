package sharedbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

func (b *SharedBiz) GetResourceURLByID(ctx context.Context, resourceID uuid.UUID) null.String {
	if resourceID == uuid.Nil {
		return null.String{}
	}

	rs, err := b.storage.GetSharedResource(ctx, db.GetSharedResourceParams{
		ID: pgutil.UUIDToPgUUID(resourceID),
	})
	if err != nil {
		return null.String{}
	}

	return null.StringFrom(b.MustGetFileURL(ctx, rs.Provider, rs.ObjectKey))
}
