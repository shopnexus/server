package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

// photoTTL is how long a presigned photo URL reads.
const photoTTL = time.Hour

// SavePhoto is UC-SHR-01 A3: it writes the photo, then its row, after
// scheduling the expiry as CreateUpload does. A photo already taken by a
// listing is left alone: its original is gone after processing, and writing
// it again would leave an object nothing deletes.
func (m *Media) SavePhoto(ctx context.Context, owner, id uuid.UUID, data []byte, contentType string, origin domain.Origin) error {
	if origin == domain.OriginApp {
		return domain.ErrInvalidUpload
	}
	existing, err := m.d.Uploads.Get(ctx, id)
	switch {
	case err == nil && existing.OwnerID() != owner:
		return domain.ErrInvalidUpload
	case err == nil && existing.Status() != domain.StatusWaiting:
		return nil
	case err != nil && !errors.Is(err, repository.ErrNotFound):
		return fmt.Errorf("get upload: %w", err)
	}
	u, err := domain.NewUpload(id, owner, contentType, int64(len(data)), origin, m.d.Now())
	if err != nil {
		return err
	}
	if _, err = ingress.ServiceSend[uuid.UUID](m.d.Ingress, m.d.Prefix+mediaExpire, "Run").
		Send(ctx, id, restate.WithDelay(domain.ExpireAfter), restate.WithIdempotencyKey(id.String())); err != nil {
		return fmt.Errorf("schedule the photo's expiry: %w", err)
	}
	if err = m.d.Store.Put(ctx, u.Key(), u.ContentType(), bytes.NewReader(data), u.Size()); err != nil {
		return fmt.Errorf("put the photo: %w", err)
	}
	if err = m.d.Uploads.Create(ctx, u); err != nil {
		return fmt.Errorf("create upload: %w", err)
	}
	return nil
}

// HoldVideo is UC-SHR-01 A5: Attach for one video, in shape video.
func (m *Media) HoldVideo(ctx context.Context, owner, holder, id, writeID uuid.UUID) error {
	return m.attachAs(ctx, owner, holder, domain.ShapeVideo, []uuid.UUID{id}, writeID)
}

func (m *Media) VideoKey(ctx context.Context, id uuid.UUID) (string, error) {
	u, err := m.d.Uploads.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return "", objectstore.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get upload: %w", err)
	}
	if u.Shape() != domain.ShapeVideo || u.Status() != domain.StatusAttached {
		return "", objectstore.ErrNotFound
	}
	return u.Key(), nil
}

func (m *Media) PresignPhoto(ctx context.Context, id uuid.UUID) (string, error) {
	url, err := m.d.Store.PresignGet(ctx, domain.UploadKey(id), photoTTL)
	if err != nil {
		return "", fmt.Errorf("presign the photo: %w", err)
	}
	return url, nil
}

// Consume is UC-SHR-01 A5: it deletes the object before it saves the row: a
// retry after a failed save finds the row still held, deletes nothing that
// is there, and saves.
func (m *Media) Consume(ctx context.Context, id, writeID uuid.UUID) error {
	u, err := m.d.Uploads.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get upload: %w", err)
	}
	if !u.Consume(m.d.Now()) {
		return nil
	}
	if err = m.d.Store.Delete(ctx, u.Key()); err != nil {
		return fmt.Errorf("delete the video: %w", err)
	}
	if err = m.d.Uploads.Save(ctx, &u, writeID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return apperr.ErrTryAgain
		}
		return fmt.Errorf("save upload: %w", err)
	}
	return nil
}
