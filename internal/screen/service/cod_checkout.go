package service

import (
	"context"
	"fmt"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
)

type ShippingEstimate struct {
	Low  int64 `json:"low"`
	High int64 `json:"high"`
}

type CodCheckoutScreen struct {
	Deal             dealsvc.DealView        `json:"deal"`
	Seller           chatsvc.PersonView      `json:"seller"`
	Address          *accountsvc.AddressView `json:"address"`
	CodOrders        Cap                     `json:"cod_orders"`
	ShippingEstimate *ShippingEstimate       `json:"shipping_estimate"`
}

func (s *Screen) CodCheckout(ctx context.Context, account, deal uuid.UUID, address *uuid.UUID) (CodCheckoutScreen, error) {
	d, err := s.d.Deals.Get(ctx, account, deal)
	if err != nil {
		return CodCheckoutScreen{}, fmt.Errorf("read deal: %w", err)
	}
	if d.Role != dealdomain.Buyer {
		return CodCheckoutScreen{}, dealdomain.ErrNotFound
	}
	var selected *accountsvc.AddressView
	if address != nil {
		a, addressErr := s.d.Accounts.Address(ctx, account, *address)
		if addressErr != nil {
			return CodCheckoutScreen{}, fmt.Errorf("read selected address: %w", addressErr)
		}
		selected = &a
	} else {
		book, bookErr := s.d.Accounts.Addresses(ctx, account)
		if bookErr != nil {
			return CodCheckoutScreen{}, fmt.Errorf("read address book: %w", bookErr)
		}
		if len(book) > 0 {
			selected = &book[0]
		}
	}
	person, err := s.person(ctx, d.SellerID)
	if err != nil {
		return CodCheckoutScreen{}, err
	}
	orders, err := s.d.Deals.BuyerOrders(ctx, account)
	if err != nil {
		return CodCheckoutScreen{}, fmt.Errorf("count COD orders: %w", err)
	}
	return CodCheckoutScreen{
		Deal: d, Seller: person, Address: selected,
		CodOrders: Cap{Used: orders.Open, Max: dealdomain.MaxOpenOrders},
	}, nil
}
