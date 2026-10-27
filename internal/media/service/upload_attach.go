package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/objectstore"
)

// Attach gives owner's uploads to holder as photos, all or none.
func (m *Media) Attach(ctx context.Context, owner, holder uuid.UUID, ids []uuid.UUID) error {
	return m.attachAs(ctx, owner, holder, domain.ShapePhoto, ids)
}

// attachAs gives owner's uploads to holder in shape, all or none. Each must
// be owner's and either held by holder in shape already or waiting and put
// to the store with the type and size it declared.
func (m *Media) attachAs(ctx context.Context, owner, holder uuid.UUID, shape domain.Shape, ids []uuid.UUID) error {
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
		if u.Status() != domain.StatusWaiting {
			if _, err = u.Attach(owner, holder, shape, now); err != nil {
				return err
			}
			continue
		}
		size, contentType, statErr := m.store.Stat(ctx, u.Key())
		if errors.Is(statErr, objectstore.ErrNotFound) {
			return domain.ErrInvalidUpload
		}
		if statErr != nil {
			return fmt.Errorf("stat upload: %w", statErr)
		}
		if size != u.Size() || contentType != u.ContentType() {
			return domain.ErrInvalidUpload
		}
		if _, err = u.Attach(owner, holder, shape, now); err != nil {
			return err
		}
		ups = append(ups, u)
	}
	if len(ups) == 0 {
		return nil
	}
	if err = m.uploads.SaveAll(ctx, ups, uuid.NewV7()); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return apperr.ErrTryAgain
		}
		return fmt.Errorf("save uploads: %w", err)
	}
	return nil
}
