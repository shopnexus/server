package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// ListActions lists the audit log newest first, narrowed by f.
func (s *Staff) ListActions(ctx context.Context, f audit.Filter, p page.Request) (page.Page[audit.Action], error) {
	rows, err := s.actions.List(ctx, f, p)
	if err != nil {
		return page.Page[audit.Action]{}, fmt.Errorf("list staff actions: %w", err)
	}
	return page.Of(rows, p, func(a audit.Action) uuid.UUID { return a.ID }), nil
}
