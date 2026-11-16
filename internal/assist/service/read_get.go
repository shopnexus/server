package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

// GetRead is UC-LST-01, step 2: it returns one of account's reads with a
// fresh URL for each photo.
func (a *Assist) GetRead(ctx context.Context, account, id uuid.UUID) (ListingReadView, error) {
	r, err := a.d.Reads.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return ListingReadView{}, domain.ErrNotFound
	}
	if err != nil {
		return ListingReadView{}, fmt.Errorf("get read: %w", err)
	}
	if r.AccountID() != account || r.Expired(a.d.Now()) {
		return ListingReadView{}, domain.ErrNotFound
	}
	v := ListingReadView{
		ID: r.ID(), Source: r.Source(), Status: r.Status(), Failure: r.Failure(), Draft: r.Draft(), ExpiresAt: r.ExpiresAt(),
	}
	if r.Source() == domain.SourceLink {
		l := r.Link()
		v.Link = &LinkSourceView{Site: l.Site, URL: l.URL, PhotoCount: r.LinkPhotos()}
		if v.Draft != nil {
			removed := r.ContactRemoved()
			v.ContactRemoved = &removed
		}
	}
	for _, p := range r.Photos() {
		url, signErr := a.d.Media.PresignPhoto(ctx, p.UploadID)
		if signErr != nil {
			return ListingReadView{}, fmt.Errorf("sign photo %s: %w", p.UploadID, signErr)
		}
		v.Photos = append(v.Photos, PhotoView{UploadID: p.UploadID, URL: url, AtSeconds: p.AtSeconds})
	}
	if r.Status() == domain.StatusDone {
		est := r.Estimate()
		v.Estimate = &est
	}
	return v, nil
}
