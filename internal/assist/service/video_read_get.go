package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

// GetVideoRead returns one of account's reads with a fresh URL for each frame.
func (a *Assist) GetVideoRead(ctx context.Context, account, id uuid.UUID) (VideoReadView, error) {
	r, err := a.d.VideoReads.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return VideoReadView{}, domain.ErrNotFound
	}
	if err != nil {
		return VideoReadView{}, fmt.Errorf("get read: %w", err)
	}
	if r.AccountID() != account || r.Expired(a.d.Now()) {
		return VideoReadView{}, domain.ErrNotFound
	}
	v := VideoReadView{ID: r.ID(), Status: r.Status(), Failure: r.Failure(), Draft: r.Draft(), ExpiresAt: r.ExpiresAt()}
	for _, f := range r.Frames() {
		url, signErr := a.d.Media.PresignPhoto(ctx, f.UploadID)
		if signErr != nil {
			return VideoReadView{}, fmt.Errorf("sign frame %s: %w", f.UploadID, signErr)
		}
		v.Frames = append(v.Frames, FrameView{UploadID: f.UploadID, URL: url, AtSeconds: f.AtSeconds})
	}
	if r.Status() == domain.StatusDone {
		est := r.Estimate()
		v.Estimate = &est
	}
	return v, nil
}
