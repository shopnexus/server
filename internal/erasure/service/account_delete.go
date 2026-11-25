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
// cancelled by deal's worker on account.deleted.
func (s *Erasure) DeleteAccount(ctx context.Context, p authn.Principal) error {
	if s.d.Now().Sub(p.AuthTime) > accountdomain.RecentAuth {
		return accountdomain.ErrReauthRequired
	}
	open, err := s.d.Deals.OpenOrders(ctx, p.AccountID)
	if err != nil {
		return fmt.Errorf("check open orders: %w", err)
	}
	if open {
		return dealdomain.ErrOrdersOpen
	}
	if err = s.d.Accounts.Erase(ctx, p.AccountID); err != nil {
		return fmt.Errorf("erase account: %w", err)
	}
	return nil
}
