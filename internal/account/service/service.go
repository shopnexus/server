// Package service holds module account's use cases. Its handlers and the
// modules auth and staff call them through Service.
package service

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Service is module account's contract.
//
//nolint:interfacebloat // One method per use case of accounts, suspensions and blocks.
type Service interface {
	// Get reads an account.
	Get(ctx context.Context, id uuid.UUID) (AccountView, error)
	// Rename sets the display name of an account that may write.
	Rename(ctx context.Context, id uuid.UUID, name string) (AccountView, error)
	// Create inserts an active account; creating the same id twice does
	// nothing.
	Create(ctx context.Context, id uuid.UUID, displayName string) error
	// Delete removes an account; only the sign-up compensation calls it.
	Delete(ctx context.Context, id uuid.UUID) error
	// Status is the status in force now.
	Status(ctx context.Context, id uuid.UUID) (domain.Status, error)
	// Record reads one account as staff see it.
	Record(ctx context.Context, id uuid.UUID) (Record, error)
	// Records lists accounts newest first: the p.Fetch() rows read, for the
	// caller to cut into a page.
	Records(ctx context.Context, p page.Request) ([]Record, error)
	// Suspend stops an account from writing and records account.suspend.
	Suspend(ctx context.Context, in SuspendInput) (Record, error)
	// Reinstate lifts a suspension and records account.unsuspend.
	Reinstate(ctx context.Context, id uuid.UUID, by audit.Actor) (Record, error)
	// Block stops blocked and blocker from reaching each other; blocking
	// again changes nothing.
	Block(ctx context.Context, blocker, blocked uuid.UUID) error
	Unblock(ctx context.Context, blocker, blocked uuid.UUID) error
	// Blocked reports a block between a and b, in either direction.
	Blocked(ctx context.Context, a, b uuid.UUID) (bool, error)
	// Blocks lists the accounts blocker blocks, newest block first.
	Blocks(ctx context.Context, blocker uuid.UUID, p page.Request) (page.Page[BlockView], error)
}

// Account implements Service in this process.
type Account struct {
	accounts repository.Accounts
	now      func() time.Time
}

var _ Service = (*Account)(nil)

func New(accounts repository.Accounts, now func() time.Time) *Account {
	return &Account{accounts: accounts, now: now}
}
