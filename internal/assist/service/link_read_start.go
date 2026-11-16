package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

// StartLinkRead is UC-LST-01 A2: it parses the link, runs LinkRead.Start
// once per account and idempotencyKey, then reads the read it started.
func (a *Assist) StartLinkRead(ctx context.Context, account uuid.UUID, idempotencyKey, link string) (ListingReadView, error) {
	l, err := domain.ParseLink(link)
	if err != nil {
		return ListingReadView{}, err
	}
	res, err := a.startLink.Call(ctx, account.String()+"/"+idempotencyKey, StartLinkInput{Account: account, Link: l})
	if err != nil {
		return ListingReadView{}, err
	}
	return a.GetRead(ctx, account, res.ReadID)
}
