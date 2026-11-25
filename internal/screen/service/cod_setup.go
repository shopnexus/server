package service

import (
	"context"
	"fmt"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	shippingdomain "github.com/shopnexus/server/internal/shipping/domain"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
)

type CodSetupScreen struct {
	Account         accountsvc.MeView        `json:"account"`
	Addresses       []accountsvc.AddressView `json:"addresses"`
	Settings        shippingdomain.Settings  `json:"settings"`
	CarrierAccounts shippingsvc.AccountList  `json:"carrier_accounts"`
}

func (s *Screen) CodSetup(ctx context.Context, account uuid.UUID) (CodSetupScreen, error) {
	me, err := s.d.Accounts.Me(ctx, account)
	if err != nil {
		return CodSetupScreen{}, fmt.Errorf("read account: %w", err)
	}
	book, err := s.d.Accounts.Addresses(ctx, account)
	if err != nil {
		return CodSetupScreen{}, fmt.Errorf("read addresses: %w", err)
	}
	settings, err := s.d.Shipping.Settings(ctx, account)
	if err != nil {
		return CodSetupScreen{}, fmt.Errorf("read shipping settings: %w", err)
	}
	carriers, err := s.d.Shipping.Accounts(ctx, account)
	if err != nil {
		return CodSetupScreen{}, fmt.Errorf("read carrier accounts: %w", err)
	}
	return CodSetupScreen{Account: me, Addresses: book, Settings: settings, CarrierAccounts: carriers}, nil
}
