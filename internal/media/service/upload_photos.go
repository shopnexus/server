package service

import (
	"context"
	"fmt"
	"uuid"

	"golang.org/x/sync/errgroup"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/media/domain"
)

// Photos is UC-SHR-01, steps 3 and 4: it attaches owner's uploads to holder
// as photos and runs MediaProcess on each at once, with the upload ID as
// the idempotency key, so a retry and a run already under way share one
// processing. It returns each photo's keys by upload ID; a photo processing
// refuses is domain.ErrInvalidUpload.
func (m *Media) Photos(ctx context.Context, owner, holder uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]ProcessResult, error) {
	if err := m.attachAs(ctx, owner, holder, domain.ShapePhoto, ids, uuid.NewV7()); err != nil {
		return nil, err
	}
	results := make([]ProcessResult, len(ids))
	g, gctx := errgroup.WithContext(ctx)
	for i, id := range ids {
		g.Go(func() error {
			res, err := m.process.Call(gctx, id, restate.WithIdempotencyKey(id.String()))
			if err != nil {
				return fmt.Errorf("process photo %s: %w", id, err)
			}
			results[i] = res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]ProcessResult, len(ids))
	for i, id := range ids {
		if results[i].Status != domain.StatusReady {
			return nil, domain.ErrInvalidUpload
		}
		out[id] = results[i]
	}
	return out, nil
}
