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
)

// Extend is UC-SHR-01 A1: it keeps owner's photos for their drafts, all or
// none: each must be owner's photo, waiting before its expiry and put to
// the store with the type and size it declared. Each then expires
// domain.ExtendFor from now, or later if it did already.
func (m *Media) Extend(ctx context.Context, owner uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := m.d.Now()
	ids = slices.Clone(ids)
	slices.SortFunc(ids, uuid.UUID.Compare)
	ids = slices.Compact(ids)
	found, err := m.d.Uploads.GetMany(ctx, ids)
	if err != nil {
		return fmt.Errorf("get uploads: %w", err)
	}
	if len(found) != len(ids) {
		return domain.ErrInvalidUpload
	}
	ups := make([]*domain.Upload, len(found))
	for i := range found {
		u := &found[i]
		if err = u.Extend(owner, now); err != nil {
			return err
		}
		if err = m.checkStored(ctx, u); err != nil {
			return err
		}
		ups[i] = u
	}
	if err = m.d.Uploads.SaveAll(ctx, ups, uuid.NewV7()); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return apperr.ErrTryAgain
		}
		return fmt.Errorf("save uploads: %w", err)
	}
	return nil
}

// Gone returns those of ids that owner's drafts can no longer name: an
// upload of owner's a holder took, one that expired or waits past its
// expiry, and an ID with no upload, in the order of ids.
func (m *Media) Gone(ctx context.Context, owner uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	now := m.d.Now()
	found, err := m.d.Uploads.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get uploads: %w", err)
	}
	byID := make(map[uuid.UUID]*domain.Upload, len(found))
	for i := range found {
		byID[found[i].ID()] = &found[i]
	}
	var gone []uuid.UUID
	for _, id := range ids {
		if u, ok := byID[id]; !ok || u.Gone(owner, now) {
			gone = append(gone, id)
		}
	}
	return gone, nil
}
