package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

// Attach gives owner's uploads to holder as photos, all or none.
func (m *Media) Attach(ctx context.Context, owner, holder uuid.UUID, ids []uuid.UUID, writeID uuid.UUID) error {
	return m.attachAs(ctx, owner, holder, domain.ShapePhoto, ids, writeID)
}

// attachAs gives owner's uploads to holder in shape, all or none. Each must
// be owner's and either held by holder in shape already or waiting and put
// to the store with the type and size it declared. The changed uploads
// save under writeID, which the caller mints so a retry sends the same.
func (m *Media) attachAs(ctx context.Context, owner, holder uuid.UUID, shape domain.Shape, ids []uuid.UUID, writeID uuid.UUID) error {
	now := m.now()
	ids = slices.Clone(ids)
	slices.SortFunc(ids, uuid.UUID.Compare)
	ids = slices.Compact(ids)
	found, err := m.uploads.GetMany(ctx, ids)
	if err != nil {
		return fmt.Errorf("get uploads: %w", err)
	}
	if len(found) != len(ids) {
		return domain.ErrInvalidUpload
	}
	ups := make([]*domain.Upload, 0, len(found))
	for i := range found {
		u := &found[i]
		changed, attachErr := u.Attach(owner, holder, shape, now)
		if attachErr != nil {
			return attachErr
		}
		if !changed {
			continue
		}
		if err = m.checkStored(ctx, u); err != nil {
			return err
		}
		ups = append(ups, u)
	}
	if len(ups) == 0 {
		return nil
	}
	if err = m.uploads.SaveAll(ctx, ups, writeID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return apperr.ErrTryAgain
		}
		return fmt.Errorf("save uploads: %w", err)
	}
	return nil
}

// checkStored checks that upload u was put to the store with the type and
// size it declared; anything else is domain.ErrInvalidUpload. attachAs and
// Extend call it for each upload they change.
func (m *Media) checkStored(ctx context.Context, u *domain.Upload) error {
	size, contentType, err := m.store.Stat(ctx, u.Key())
	if errors.Is(err, objectstore.ErrNotFound) {
		return domain.ErrInvalidUpload
	}
	if err != nil {
		return fmt.Errorf("stat upload: %w", err)
	}
	if size != u.Size() || contentType != u.ContentType() {
		return domain.ErrInvalidUpload
	}
	return nil
}
