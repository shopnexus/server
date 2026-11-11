package service

import (
	"context"
	"uuid"
)

// StartPhotoRead runs PhotoRead.Start once per account and idempotencyKey,
// then reads the read it started.
func (a *Assist) StartPhotoRead(ctx context.Context, account uuid.UUID, idempotencyKey string, photo uuid.UUID) (ListingReadView, error) {
	res, err := a.startPhoto.Call(ctx, account.String()+"/"+idempotencyKey, StartPhotoInput{Account: account, Photo: photo})
	if err != nil {
		return ListingReadView{}, err
	}
	return a.GetRead(ctx, account, res.ReadID)
}
