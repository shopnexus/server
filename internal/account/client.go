package account

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Client implements the ports other modules declare for accounts.
type Client struct {
	Store Store
	Now   func() time.Time
}

func (c Client) Create(ctx context.Context, id uuid.UUID, displayName string) error {
	return Create(ctx, c.Store, id, displayName, c.Now())
}

// Delete removes an account; see Store.Delete.
func (c Client) Delete(ctx context.Context, id uuid.UUID) error {
	if err := c.Store.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

func (c Client) Status(ctx context.Context, id uuid.UUID) (Status, error) {
	return StatusOf(ctx, c.Store, id, c.Now())
}

func (c Client) Record(ctx context.Context, id uuid.UUID) (Record, error) {
	return GetRecord(ctx, c.Store, id, c.Now())
}

func (c Client) Records(ctx context.Context, p page.Request) ([]Record, error) {
	return Records(ctx, c.Store, p, c.Now())
}

func (c Client) Suspend(ctx context.Context, id uuid.UUID, reason string, until *time.Time, by audit.Actor) (Record, error) {
	return Suspend(ctx, c.Store, id, reason, until, by, c.Now())
}

func (c Client) Reinstate(ctx context.Context, id uuid.UUID, by audit.Actor) (Record, error) {
	return Reinstate(ctx, c.Store, id, by, c.Now())
}
