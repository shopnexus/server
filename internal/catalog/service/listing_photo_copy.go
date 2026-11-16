package service

import (
	"context"
	"fmt"
	"uuid"
)

// CopyPhotos is UC-LST-01 A5: it runs ListingPhotoCopy once per seller,
// listing and idempotencyKey, then signs a URL for each copy. The listing
// keeps its own photos, and a new listing or draft takes the copies like
// photos the app put. A repeated key gets the first run's uploads with
// fresh URLs. Another seller's listing, or a deleted one, is
// domain.ErrNotFound.
func (c *Catalog) CopyPhotos(ctx context.Context, seller, id uuid.UUID, idempotencyKey string) ([]DraftPhotoView, error) {
	res, err := c.photoCopy.Call(ctx, seller.String()+"/"+id.String()+"/"+idempotencyKey, PhotoCopyInput{Seller: seller, Listing: id})
	if err != nil {
		return nil, err
	}
	out := make([]DraftPhotoView, 0, len(res.Uploads))
	for _, upload := range res.Uploads {
		url, presignErr := c.media.PresignPhoto(ctx, upload)
		if presignErr != nil {
			return nil, fmt.Errorf("presign a copy: %w", presignErr)
		}
		out = append(out, DraftPhotoView{UploadID: upload, URL: url})
	}
	return out, nil
}
