package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/media/domain"
)

// Square attaches owner's upload to holder as a square and runs
// MediaProcess on it, with the upload ID as the idempotency key, so a retry
// and a run already under way share one processing. A photo processing
// refuses is domain.ErrInvalidUpload.
func (m *Media) Square(ctx context.Context, owner, holder, id uuid.UUID) (string, error) {
	if err := m.attachAs(ctx, owner, holder, domain.ShapeSquare, []uuid.UUID{id}); err != nil {
		return "", err
	}
	res, err := m.process.Call(ctx, id.String(), id)
	if err != nil {
		return "", fmt.Errorf("process the square: %w", err)
	}
	if res.Status != domain.StatusReady {
		return "", domain.ErrInvalidUpload
	}
	return res.PhotoKey, nil
}
