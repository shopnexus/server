package service

import (
	"context"
	"uuid"
)

// StartPhotoRead is UC-LST-01, step 2: a key the account started a read
// with already answers that read; otherwise it runs PhotoRead.Start, then
// reads the read it started.
func (a *Assist) StartPhotoRead(ctx context.Context, account, key, photo uuid.UUID) (ListingReadView, error) {
	if v, ok, err := a.readOfKey(ctx, account, key); ok || err != nil {
		return v, err
	}
	res, err := a.startPhoto.Call(ctx, "", StartPhotoInput{Account: account, Key: key, Photo: photo})
	if err != nil {
		return ListingReadView{}, err
	}
	return a.GetRead(ctx, account, res.ReadID)
}
