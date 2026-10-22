package account

import (
	"context"
	"fmt"
	"time"
	"uuid"
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
