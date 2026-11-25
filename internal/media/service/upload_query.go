package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/imaging"
	"github.com/shopnexus/server/internal/media/repository"
)

// QueryUpload is a photo a person searches with.
type QueryUpload struct {
	ExpiresAt time.Time
}

func (m *Media) QueryUpload(ctx context.Context, owner, id uuid.UUID) (QueryUpload, error) {
	u, err := m.searchPhoto(ctx, owner, id)
	if err != nil {
		return QueryUpload{}, err
	}
	return QueryUpload{ExpiresAt: u.ExpiresAt()}, nil
}

// searchPhoto reads owner's search photo id, checked as QueryUpload
// promises.
func (m *Media) searchPhoto(ctx context.Context, owner, id uuid.UUID) (domain.Upload, error) {
	u, err := m.d.Uploads.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Upload{}, domain.ErrInvalidUpload
	}
	if err != nil {
		return domain.Upload{}, fmt.Errorf("get upload: %w", err)
	}
	if !u.Queryable(owner, m.d.Now()) || domain.IsVideo(u.ContentType()) {
		return domain.Upload{}, domain.ErrInvalidUpload
	}
	if err = m.checkStored(ctx, &u); err != nil {
		return domain.Upload{}, err
	}
	return u, nil
}

// QueryImage decodes in one of the process's slots, as MediaProcess does, so
// searches and listings together decode at most processSlots photos.
func (m *Media) QueryImage(ctx context.Context, owner, id uuid.UUID) ([]byte, error) {
	u, err := m.searchPhoto(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	r, err := m.d.Store.Get(ctx, u.Key())
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	defer func() { _ = r.Close() }()
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for a processing slot: %w", ctx.Err())
	}
	defer func() { <-m.slots }()
	photo, _, err := imaging.Process(r, u.ContentType(), domain.ShapeQuery)
	if _, refused := errors.AsType[imaging.RefusedError](err); refused {
		return nil, domain.ErrInvalidUpload
	}
	if err != nil {
		return nil, fmt.Errorf("shrink the photo: %w", err)
	}
	return photo, nil
}

func (m *Media) ReadPhoto(ctx context.Context, key string) ([]byte, error) {
	r, err := m.d.Store.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(io.LimitReader(r, domain.MaxPhotoBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	return b, nil
}
