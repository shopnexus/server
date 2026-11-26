package service

import (
	"context"
	"fmt"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/authn"
)

// DeleteAccount checks the sign-in's age and the open orders, then erases
// the account. A COD request that lands between the check and the erase is
// cancelled by deal's worker on account.deleted (A1).
//
// DeleteAccount is UC-ACC-09.
func (s *Erasure) DeleteAccount(ctx context.Context, p authn.Principal) error {
	// E1 · BR-ACC-04
	if s.d.Now().Sub(p.AuthTime) > accountdomain.RecentAuth {
		return accountdomain.ErrReauthRequired
	}
	// 2. No order of theirs is requested or shipped; E2 otherwise.
	open, err := s.d.Deals.OpenOrders(ctx, p.AccountID)
	if err != nil {
		return fmt.Errorf("check open orders: %w", err)
	}
	if open {
		return dealdomain.ErrOrdersOpen
	}
	// 3. Erase, or E3 while suspended.
	if err = s.d.Accounts.Erase(ctx, p.AccountID); err != nil {
		return fmt.Errorf("erase account: %w", err)
	}
	return nil
}
