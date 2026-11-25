package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
)

// ListActions lists the audit log newest first, narrowed by f.
func (s *Console) ListActions(ctx context.Context, f audit.Filter, p page.Request) (page.Page[audit.Action], error) {
	rows, err := s.d.Actions.List(ctx, f, p)
	if err != nil {
		return page.Page[audit.Action]{}, fmt.Errorf("list console actions: %w", err)
	}
	return page.Of(rows, p, func(a audit.Action) uuid.UUID { return a.ID }), nil
}
