package sharedbiz

import (
	"context"
	"fmt"
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

func (b *SharedBiz) GetResourceURLByID(ctx context.Context, resourceID int64) null.String {
	if resourceID == 0 {
		return null.String{}
	}

	rs, err := b.storage.GetSharedResource(ctx, db.GetSharedResourceParams{
		ID: pgutil.Int64ToPgInt8(resourceID),
	})
	if err != nil {
		return null.String{}
	}

	return null.StringFrom(GetResourceURL(rs.Code))
}

func GetResourceURL(resourceCode string) string {
	switch config.GetConfig().Filestore.Type {
	case "local":
		return fmt.Sprintf("%s/api/v1/shared/files/%s", config.GetConfig().App.PublicURL, resourceCode)
	case "s3":
		return fmt.Sprintf("https://%s/%s", config.GetConfig().Filestore.S3.CloudfrontURL, resourceCode)
	default:
		return "" // TODO: add 404 link
	}
}
