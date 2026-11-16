package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

// PutDraft is UC-LST-01 A4: it writes the caller's draft id; the last write
// wins. It drops the photos a listing took or that expired, keeps the
// others 30 days in media, then writes the row. Media and catalog share no
// transaction: an extension whose write then fails only keeps a photo
// longer.
func (c *Catalog) PutDraft(ctx context.Context, account, id uuid.UUID, content domain.DraftContent) (DraftView, bool, error) {
	now := c.now()
	gone, err := c.media.Gone(ctx, account, content.Photos)
	if err != nil {
		return DraftView{}, false, fmt.Errorf("find the photos that are gone: %w", err)
	}
	content.Photos = domain.KeepPhotos(content.Photos, gone)
	if err = c.media.Extend(ctx, account, content.Photos); err != nil {
		return DraftView{}, false, fmt.Errorf("keep the photos: %w", err)
	}
	d, created, err := c.drafts.Put(ctx, account, id, content, now)
	if err != nil {
		return DraftView{}, false, fmt.Errorf("put draft: %w", err)
	}
	v, err := c.draftView(ctx, d, nil)
	return v, created, err
}

// ListDrafts is UC-LST-01 A4: it reads the caller's drafts, last written
// first, once their expired drafts are deleted. A photo a listing took or
// that expired is left out of the answer, without a write to the draft.
func (c *Catalog) ListDrafts(ctx context.Context, account uuid.UUID) ([]DraftView, error) {
	ds, err := c.drafts.List(ctx, account, c.now())
	if err != nil {
		return nil, fmt.Errorf("list drafts: %w", err)
	}
	var all []uuid.UUID
	for i := range ds {
		all = append(all, ds[i].Photos()...)
	}
	gone, err := c.media.Gone(ctx, account, all)
	if err != nil {
		return nil, fmt.Errorf("find the photos that are gone: %w", err)
	}
	out := make([]DraftView, len(ds))
	for i := range ds {
		if out[i], err = c.draftView(ctx, ds[i], gone); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// DeleteDraft is UC-LST-01 A4: it deletes the caller's draft id; another
// account's draft, or none, changes nothing. Its photos expire on their
// own.
func (c *Catalog) DeleteDraft(ctx context.Context, account, id uuid.UUID) error {
	if err := c.drafts.Delete(ctx, account, id); err != nil {
		return fmt.Errorf("delete draft: %w", err)
	}
	return nil
}

// draftView is d without the photos in gone, each other photo with a
// presigned URL valid for an hour. PutDraft and ListDrafts call it.
func (c *Catalog) draftView(ctx context.Context, d domain.ListingDraft, gone []uuid.UUID) (DraftView, error) {
	dc := d.Content()
	photos := domain.KeepPhotos(dc.Photos, gone)
	v := DraftView{
		ID: d.ID(), SchemaVersion: dc.SchemaVersion, Title: dc.Title, Price: dc.Price, Body: dc.Body,
		Photos: make([]DraftPhotoView, 0, len(photos)), UpdatedAt: d.UpdatedAt(), ExpiresAt: d.ExpiresAt(),
	}
	for _, id := range photos {
		url, err := c.media.PresignPhoto(ctx, id)
		if err != nil {
			return DraftView{}, fmt.Errorf("presign a photo: %w", err)
		}
		v.Photos = append(v.Photos, DraftPhotoView{UploadID: id, URL: url})
	}
	return v, nil
}
