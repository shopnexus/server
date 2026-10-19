package account

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/postgres"
)

// Module is the account module's entry point for other modules and its own
// use cases.
type Module struct {
	store Store
	now   func() time.Time
}

func NewModule(store Store, now func() time.Time) *Module {
	return &Module{store: store, now: now}
}

// Create inserts an active account on q, so auth can create it in the same
// transaction as the person's first sign-in method.
func (m *Module) Create(ctx context.Context, q postgres.Querier, displayName string) (uuid.UUID, error) {
	a, err := New(displayName, m.now())
	if err != nil {
		return uuid.UUID{}, err
	}
	if err := m.store.Insert(ctx, q, &a); err != nil {
		return uuid.UUID{}, err
	}
	return a.ID(), nil
}

// Status returns the status in force now.
func (m *Module) Status(ctx context.Context, q postgres.Querier, id uuid.UUID) (Status, error) {
	a, err := m.store.Get(ctx, q, id)
	if err != nil {
		return "", err
	}
	return a.Status(m.now()), nil
}
