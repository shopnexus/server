// Package service holds module erasure's one use case: a person deletes
// their account. It sits above account and deal, since deal imports
// account and account cannot ask deal about open orders.
package service

import (
	"context"
	"time"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Service is module erasure's contract.
type Service interface {
	// DeleteAccount deletes the caller's account after a recent sign-in,
	// unless an order of theirs is requested or shipped.
	DeleteAccount(ctx context.Context, p authn.Principal) error
}

// Deps is what New builds Erasure from.
type Deps struct {
	Accounts accountsvc.Service
	Deals    dealsvc.Service
	Now      func() time.Time
}

// Erasure implements Service in this process.
type Erasure struct {
	accounts accountsvc.Service
	deals    dealsvc.Service
	now      func() time.Time
}

var _ Service = (*Erasure)(nil)

func New(d Deps) *Erasure { return &Erasure{accounts: d.Accounts, deals: d.Deals, now: d.Now} }
