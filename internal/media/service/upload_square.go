package service

import (
	"context"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/media/domain"
)

// Square is UC-SHR-01 A2: it attaches owner's upload to holder as a square
// and runs MediaProcess on it, with the upload ID as the idempotency key,
// so a retry and a run already under way share one processing. A photo
// processing refuses is domain.ErrInvalidUpload.
func (m *Media) Square(ctx context.Context, owner, holder, id uuid.UUID) (string, error) {
	if err := m.attachAs(ctx, owner, holder, domain.ShapeSquare, []uuid.UUID{id}, uuid.NewV7()); err != nil {
		return "", err
	}
	res, err := m.process.Call(ctx, id, restate.WithIdempotencyKey(id.String()))
	if err != nil {
		return "", fmt.Errorf("process the square: %w", err)
	}
	if res.Status != domain.StatusReady {
		return "", domain.ErrInvalidUpload
	}
	return res.PhotoKey, nil
}
