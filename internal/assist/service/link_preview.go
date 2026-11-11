package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/provider/chotot"
)

// PreviewLink checks the account, parses the link and reads the listing,
// for the card the app shows under the field, and stores nothing.
func (a *Assist) PreviewLink(ctx context.Context, account uuid.UUID, link string) (LinkPreviewView, error) {
	status, err := a.d.Accounts.Status(ctx, account)
	if err != nil {
		return LinkPreviewView{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return LinkPreviewView{}, r
	}
	l, err := domain.ParseLink(link)
	if err != nil {
		return LinkPreviewView{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, a.d.CallTimeout)
	defer cancel()
	ad, err := a.d.Source.Ad(callCtx, l.ListID)
	switch {
	case errors.Is(err, chotot.ErrNotFound):
		return LinkPreviewView{}, domain.ErrListingNotFound
	case errors.Is(err, chotot.ErrFormat):
		a.d.Log.ErrorContext(ctx, "chotot format: the gateway's answer changed", "list_id", l.ListID, "err", err)
		return LinkPreviewView{}, domain.ErrSourceUnavailable.WithCause(err)
	case err != nil:
		a.d.Log.WarnContext(ctx, "chotot did not answer a preview", "list_id", l.ListID, "err", err)
		return LinkPreviewView{}, domain.ErrSourceUnavailable.WithCause(err)
	}
	v := LinkPreviewView{Site: l.Site, URL: l.URL, Title: ad.Subject, CoverURL: ad.Images[0], PhotoCount: len(ad.Images)}
	if ad.Price >= catalogdomain.MinPrice && ad.Price <= catalogdomain.MaxPrice {
		price := ad.Price
		v.Price = &price
	}
	return v, nil
}
