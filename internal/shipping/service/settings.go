package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/shipping/domain"
)

type Pickup struct {
	Name        string
	Phone       string
	Street      string
	CommuneCode string
	Commune     string
	Province    string
}

func (s *Shipping) Settings(ctx context.Context, owner uuid.UUID) (domain.Settings, error) {
	st, err := s.d.Settings.Get(ctx, owner)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("read settings: %w", err)
	}
	return st, nil
}

func (s *Shipping) PutSettings(ctx context.Context, owner uuid.UUID, st domain.Settings) (domain.Settings, error) {
	if err := st.Normalize(); err != nil {
		return domain.Settings{}, fmt.Errorf("normalize settings: %w", err)
	}
	if err := s.d.Settings.Put(ctx, owner, st); err != nil {
		return domain.Settings{}, fmt.Errorf("save settings: %w", err)
	}
	return st, nil
}

func (s *Shipping) SavedPickup(ctx context.Context, owner uuid.UUID) (Pickup, error) {
	st, err := s.Settings(ctx, owner)
	if err != nil {
		return Pickup{}, err
	}
	if st.Pickup == nil {
		return Pickup{}, domain.ErrPickupNotConfigured
	}
	a, err := s.d.Customers.Address(ctx, owner, st.Pickup.AddressID)
	if errors.Is(err, accountdomain.ErrAddressNotFound) {
		return Pickup{}, domain.ErrPickupNotConfigured
	}
	if err != nil {
		return Pickup{}, fmt.Errorf("read saved pickup address: %w", err)
	}
	person, err := s.d.Customers.Get(ctx, owner)
	if err != nil {
		return Pickup{}, fmt.Errorf("read pickup contact: %w", err)
	}
	return Pickup{
		Name: person.DisplayName, Phone: st.Pickup.Phone, Street: a.Street,
		CommuneCode: a.CommuneCode, Commune: a.Commune, Province: a.Province,
	}, nil
}
