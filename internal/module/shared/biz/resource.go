package sharedbiz

import (
	"context"
	"fmt"
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

func (b *SharedBiz) GetResourceURLByID(ctx context.Context, resourceID int64) null.String {
	if resourceID == 0 {
		return null.String{}
	}

	rs, err := b.storage.GetSharedResource(ctx, pgutil.Int64ToPgInt8(resourceID))
	if err != nil {
		return null.String{}
	}

	return null.StringFrom(GetResourceURL(string(rs.Provider), rs.ObjectKey))
}

func GetResourceURL(provider string, objectKey string) string {
	switch provider {
	case "S3":
		return fmt.Sprintf("https://%s/%s", config.GetConfig().Filestore.S3.CloudfrontURL, objectKey)
	case "Cloudinary":
		return ""
	case "local":
		return fmt.Sprintf("%s/api/v1/shared/files/%s", config.GetConfig().App.PublicURL, objectKey)
	case "Remote":
		return objectKey
	default:
		return "" // TODO: add 404 link
	}
}
