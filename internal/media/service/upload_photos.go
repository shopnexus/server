package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/media/domain"
)

// Photos reports the processing of each upload of ids, in their order; an
// unknown upload is missing from the result.
func (m *Media) Photos(ctx context.Context, ids []uuid.UUID) ([]Photo, error) {
	found, err := m.uploads.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get uploads: %w", err)
	}
	byID := make(map[uuid.UUID]domain.Upload, len(found))
	for i := range found {
		byID[found[i].ID()] = found[i]
	}
	out := make([]Photo, 0, len(ids))
	for _, id := range ids {
		u, ok := byID[id]
		if !ok {
			continue
		}
		p := Photo{UploadID: id, Status: u.Status(), Failure: u.Failure()}
		if u.Status() == domain.StatusReady {
			p.URL, p.ThumbURL = m.store.PublicURL(u.PhotoKey()), m.store.PublicURL(u.ThumbKey())
		}
		out = append(out, p)
	}
	return out, nil
}
