package service

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

// StartLinkRead parses the link, runs LinkRead.Start once per account and
// idempotencyKey, then reads the read it started.
func (a *Assist) StartLinkRead(ctx context.Context, account uuid.UUID, idempotencyKey, link string) (LinkReadView, error) {
	l, err := domain.ParseLink(link)
	if err != nil {
		return LinkReadView{}, err
	}
	res, err := a.startLink.Call(ctx, account.String()+"/"+idempotencyKey, StartLinkInput{Account: account, Link: l})
	if err != nil {
		return LinkReadView{}, err
	}
	return a.GetLinkRead(ctx, account, res.ReadID)
}
