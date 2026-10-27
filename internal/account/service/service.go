// Package service holds module account's use cases. Its handlers and the
// modules auth and staff call them through Service.
package service

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Service is module account's contract.
//
//nolint:interfacebloat // One method per use case of accounts, suspensions, blocks and addresses.
type Service interface {
	AddressBook

	// Get reads an account.
	Get(ctx context.Context, id uuid.UUID) (AccountView, error)
	// GetMany reads the accounts among ids, keyed by ID; an unknown ID is
	// left out.
	GetMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]AccountView, error)
	// Update changes the display name, bio and avatar of an account that
	// may write.
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (AccountView, error)
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
	media    mediasvc.Service
	now      func() time.Time
}

var _ Service = (*Account)(nil)

// New builds Account; media makes avatars and their URLs.
func New(accounts repository.Accounts, media mediasvc.Service, now func() time.Time) *Account {
	return &Account{accounts: accounts, media: media, now: now}
}
