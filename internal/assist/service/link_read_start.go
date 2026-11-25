package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
)

// StartLinkRead is UC-LST-01 A2: it parses the link; a key the account
// started a read with already answers that read, otherwise it runs
// LinkRead.Start, then reads the read it started.
func (a *Assist) StartLinkRead(ctx context.Context, account, key uuid.UUID, link string) (ListingReadView, error) {
	l, err := domain.ParseLink(link)
	if err != nil {
		return ListingReadView{}, err
	}
	if v, ok, keyErr := a.readOfKey(ctx, account, key); ok || keyErr != nil {
		return v, keyErr
	}
	res, err := a.startLink.Call(ctx, StartLinkInput{Account: account, Key: key, Link: l})
	if err != nil {
		return ListingReadView{}, err
	}
	return a.GetRead(ctx, account, res.ReadID)
}
