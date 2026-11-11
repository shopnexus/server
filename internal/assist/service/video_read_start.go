package service

import (
	"context"
	"uuid"
)

// StartVideoRead runs VideoRead.Start once per account and idempotencyKey, then
// reads the read it started. The invocation's key holds the account, so the
// same key sent by two accounts starts two reads.
func (a *Assist) StartVideoRead(ctx context.Context, account uuid.UUID, idempotencyKey string, video uuid.UUID) (ListingReadView, error) {
	res, err := a.startVideo.Call(ctx, account.String()+"/"+idempotencyKey, StartInput{Account: account, Video: video})
	if err != nil {
		return ListingReadView{}, err
	}
	return a.GetRead(ctx, account, res.ReadID)
}
