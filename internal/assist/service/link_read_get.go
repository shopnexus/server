package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

// GetLinkRead returns one of account's link reads with a fresh URL for each
// copied photo.
func (a *Assist) GetLinkRead(ctx context.Context, account, id uuid.UUID) (LinkReadView, error) {
	r, err := a.d.LinkReads.Get(ctx, id)
	if errors.Is(err, domain.ErrLinkReadNotFound) {
		return LinkReadView{}, domain.ErrLinkReadNotFound
	}
	if err != nil {
		return LinkReadView{}, fmt.Errorf("get link read: %w", err)
	}
	if r.AccountID() != account || r.Expired(a.d.Now()) {
		return LinkReadView{}, domain.ErrLinkReadNotFound
	}
	l := r.Link()
	v := LinkReadView{
		ID: r.ID(), Status: r.Status(), Failure: r.Failure(), Draft: r.Draft(), ExpiresAt: r.ExpiresAt(),
		Source: LinkSourceView{Site: l.Site, URL: l.URL, PhotoCount: r.PhotoCount()},
	}
	if v.Draft != nil {
		removed := r.ContactRemoved()
		v.ContactRemoved = &removed
		for _, p := range v.Draft.Photos {
			url, signErr := a.d.Media.PresignPhoto(ctx, p)
			if signErr != nil {
				return LinkReadView{}, fmt.Errorf("sign photo %s: %w", p, signErr)
			}
			v.Photos = append(v.Photos, CopyView{UploadID: p, URL: url})
		}
	}
	if r.Status() == domain.StatusDone {
		est := r.Estimate()
		v.Estimate = &est
	}
	return v, nil
}
