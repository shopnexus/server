package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

// QueryUpload is a photo a person searches with.
type QueryUpload struct {
	ExpiresAt time.Time
}

func (m *Media) QueryUpload(ctx context.Context, owner, id uuid.UUID) (QueryUpload, error) {
	u, err := m.uploads.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return QueryUpload{}, domain.ErrInvalidUpload
	}
	if err != nil {
		return QueryUpload{}, fmt.Errorf("get upload: %w", err)
	}
	if !u.Queryable(owner, m.now()) || domain.IsVideo(u.ContentType()) {
		return QueryUpload{}, domain.ErrInvalidUpload
	}
	size, contentType, err := m.store.Stat(ctx, u.Key())
	if errors.Is(err, objectstore.ErrNotFound) {
		return QueryUpload{}, domain.ErrInvalidUpload
	}
	if err != nil {
		return QueryUpload{}, fmt.Errorf("stat upload: %w", err)
	}
	if size != u.Size() || contentType != u.ContentType() {
		return QueryUpload{}, domain.ErrInvalidUpload
	}
	return QueryUpload{ExpiresAt: u.ExpiresAt()}, nil
}

// QueryImage decodes in one of the process's slots, as MediaProcess does, so
// searches and listings together decode at most processSlots photos.
func (m *Media) QueryImage(ctx context.Context, owner, id uuid.UUID) ([]byte, error) {
	if _, err := m.QueryUpload(ctx, owner, id); err != nil {
		return nil, err
	}
	u, err := m.uploads.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get upload: %w", err)
	}
	r, err := m.store.Get(ctx, u.Key())
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
	photo, _, err := process(r, u.ContentType(), domain.ShapeQuery)
	if _, refused := errors.AsType[refusedError](err); refused {
		return nil, domain.ErrInvalidUpload
	}
	if err != nil {
		return nil, fmt.Errorf("shrink the photo: %w", err)
	}
	return photo, nil
}

func (m *Media) ReadPhoto(ctx context.Context, key string) ([]byte, error) {
	r, err := m.store.Get(ctx, key)
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
