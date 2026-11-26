package service

import (
	"context"
	"errors"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/provider/chotot"
)

// Ads is UC-LST-04, step 4: it reads the linked profile's listings on Chợ
// Tốt each call.
func (c *Chotot) Ads(ctx context.Context, account uuid.UUID) (AdListView, error) {
	l, found, err := c.d.Links.Get(ctx, account)
	if err != nil {
		return AdListView{}, err //nolint:wrapcheck // the store names it
	}
	// E6
	if !found {
		return AdListView{}, domain.ErrNotLinked
	}
	adsCtx, cancel := context.WithTimeout(ctx, c.d.CallTimeout)
	ads, err := c.d.Source.Ads(adsCtx, l.ChototAccount, l.OID)
	cancel()
	if err != nil {
		if errors.Is(err, chotot.ErrFormat) {
			c.d.Log.ErrorContext(ctx, "chotot format: the gateway's answer changed", "err", err)
		}
		return AdListView{}, domain.ErrUnavailable.WithCause(err)
	}
	imps, err := c.d.Imports.List(ctx, account)
	if err != nil {
		return AdListView{}, err //nolint:wrapcheck // the store names it
	}
	now := c.d.Now()
	statuses, room, err := c.d.statuses(ctx, account, ads, imps, now)
	if err != nil {
		return AdListView{}, err
	}
	byList := make(map[int64]domain.Import, len(imps))
	for i := range imps {
		byList[imps[i].ListID] = imps[i]
	}
	out := AdListView{Items: make([]AdView, len(ads)), Room: room}
	for i, ad := range ads {
		st := statuses[i]
		v := AdView{
			ListID: ad.ListID, Title: ad.Subject, ThumbURL: nonEmpty(ad.Thumb), URL: domain.AdURL(ad.ListID),
			State: st.State, Reason: nonEmpty(st.Reason), ListingID: st.ListingID,
		}
		if ad.Price > 0 {
			v.Price = &ad.Price
		}
		if imp, ok := byList[ad.ListID]; ok {
			iv, viewErr := c.d.importView(ctx, imp, now)
			if viewErr != nil {
				return AdListView{}, viewErr
			}
			v.Import = &iv
		}
		out.Items[i] = v
	}
	return out, nil
}

// Import is UC-LST-04, step 5: it starts the imports through ChototImport.
func (c *Chotot) Import(ctx context.Context, account uuid.UUID, listIDs []int64) (ImportListView, error) {
	res, err := c.start.Call(ctx, StartInput{Account: account, ListIDs: listIDs})
	if err != nil {
		return ImportListView{}, err
	}
	now := c.d.Now()
	out := ImportListView{Items: make([]ImportView, len(res.Imports))}
	for i := range res.Imports {
		if out.Items[i], err = c.d.importView(ctx, res.Imports[i], now); err != nil {
			return ImportListView{}, err
		}
	}
	return out, nil
}

// PostDraft is UC-LST-04 A1: it posts the draft through ChototImport.
func (c *Chotot) PostDraft(ctx context.Context, account uuid.UUID, listID int64, in catalogdomain.ListingInput) (ImportView, error) {
	res, err := c.post.Call(ctx, DraftInput{Account: account, ListID: listID, Listing: in})
	if err != nil {
		return ImportView{}, err
	}
	return c.d.importView(ctx, res.Import, c.d.Now())
}
