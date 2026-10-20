package auth

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
)

// AccountPort is what auth needs from module account; account.Client
// implements it.
type AccountPort interface {
	Create(ctx context.Context, id uuid.UUID, displayName string) error
	Delete(ctx context.Context, id uuid.UUID) error
	Status(ctx context.Context, id uuid.UUID) (account.Status, error)
}
