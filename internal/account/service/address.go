package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

// AddressBook is the part of Service that keeps each account's delivery
// addresses. Only the owner reads or changes an address; any other caller
// gets domain.ErrAddressNotFound.
type AddressBook interface {
	// Addresses lists owner's addresses, the default first, then the
	// newest.
	Addresses(ctx context.Context, owner uuid.UUID) ([]AddressView, error)
	// Address reads one of owner's addresses; module deal copies it into
	// an order.
	Address(ctx context.Context, owner, id uuid.UUID) (AddressView, error)
	// CreateAddress adds an address under the request's key; the first
	// becomes the default. A key owner used already returns the address
	// that request added. An account at domain.MaxAddresses is
	// domain.ErrAddressLimit.
	CreateAddress(ctx context.Context, owner, key uuid.UUID, d domain.AddressDraft) (AddressView, error)
	// UpdateAddress replaces an address with d.
	UpdateAddress(ctx context.Context, owner, id uuid.UUID, d domain.AddressDraft) (AddressView, error)
	SetDefaultAddress(ctx context.Context, owner, id uuid.UUID) error
	// DeleteAddress removes an address; when it was the default, the newest
	// remaining one becomes the default.
	DeleteAddress(ctx context.Context, owner, id uuid.UUID) error
}

// AddressView is the Address schema in api/schemas/address.yaml.
type AddressView struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Phone       string    `json:"phone"`
	Street      string    `json:"street"`
	CommuneCode string    `json:"commune_code"`
	Commune     string    `json:"commune"`
	Province    string    `json:"province"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
}

func addressView(a domain.Address, p repository.Place) AddressView {
	return AddressView{
		ID: a.ID(), Name: a.Name(), Phone: a.Phone(), Street: a.Street(), CommuneCode: a.CommuneCode(),
		Commune: p.Commune, Province: p.Province, IsDefault: a.IsDefault(), CreatedAt: a.CreatedAt(),
	}
}

// commune reads the place of code; found is false for an unknown code.
func (s *Account) commune(ctx context.Context, code string) (place repository.Place, found bool, err error) {
	place, err = s.accounts.Commune(ctx, code)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.Place{}, false, nil
	}
	if err != nil {
		return repository.Place{}, false, fmt.Errorf("read the commune: %w", err)
	}
	return place, true, nil
}

// owned loads one of owner's addresses.
func (s *Account) owned(ctx context.Context, owner, id uuid.UUID) (domain.Address, error) {
	a, err := s.accounts.GetAddress(ctx, owner, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Address{}, domain.ErrAddressNotFound
	}
	if err != nil {
		return domain.Address{}, fmt.Errorf("get address: %w", err)
	}
	return a, nil
}

func (s *Account) Addresses(ctx context.Context, owner uuid.UUID) ([]AddressView, error) {
	rows, err := s.accounts.ListAddresses(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("list addresses: %w", err)
	}
	out := make([]AddressView, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		out = append(out, AddressView{
			ID: r.ID, Name: r.Name, Phone: r.Phone, Street: r.Street, CommuneCode: r.CommuneCode,
			Commune: r.Commune, Province: r.Province, IsDefault: r.IsDefault, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func (s *Account) Address(ctx context.Context, owner, id uuid.UUID) (AddressView, error) {
	a, err := s.owned(ctx, owner, id)
	if err != nil {
		return AddressView{}, err
	}
	place, _, err := s.commune(ctx, a.CommuneCode())
	if err != nil {
		return AddressView{}, err
	}
	return addressView(a, place), nil
}

func (s *Account) CreateAddress(ctx context.Context, owner, key uuid.UUID, d domain.AddressDraft) (AddressView, error) {
	place, found, err := s.commune(ctx, d.CommuneCode)
	if err != nil {
		return AddressView{}, err
	}
	id := uuid.NewV7()
	a, err := domain.NewAddress(id, owner, d, found, s.now())
	if err != nil {
		return AddressView{}, err
	}
	if err = s.accounts.InsertAddress(ctx, key, &a); err != nil {
		return AddressView{}, fmt.Errorf("insert address: %w", err)
	}
	if a.ID() != id {
		// A retried request: a is the address its first try added, which
		// may name another commune if the body changed.
		if place, _, err = s.commune(ctx, a.CommuneCode()); err != nil {
			return AddressView{}, err
		}
	}
	return addressView(a, place), nil
}

// UpdateAddress saves under the address's write ID, so an edit that lost a
// race with another edit or a move of the default is apperr.ErrTryAgain.
func (s *Account) UpdateAddress(ctx context.Context, owner, id uuid.UUID, d domain.AddressDraft) (AddressView, error) {
	a, err := s.owned(ctx, owner, id)
	if err != nil {
		return AddressView{}, err
	}
	place, found, err := s.commune(ctx, d.CommuneCode)
	if err != nil {
		return AddressView{}, err
	}
	if err = a.Edit(d, found); err != nil {
		return AddressView{}, err
	}
	err = s.accounts.SaveAddress(ctx, &a, uuid.NewV7())
	if errors.Is(err, repository.ErrConflict) {
		return AddressView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return AddressView{}, fmt.Errorf("save address: %w", err)
	}
	return addressView(a, place), nil
}

func (s *Account) SetDefaultAddress(ctx context.Context, owner, id uuid.UUID) error {
	err := s.accounts.SetDefaultAddress(ctx, owner, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrAddressNotFound
	}
	if err != nil {
		return fmt.Errorf("set default address: %w", err)
	}
	return nil
}

func (s *Account) DeleteAddress(ctx context.Context, owner, id uuid.UUID) error {
	err := s.accounts.DeleteAddress(ctx, owner, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrAddressNotFound
	}
	if err != nil {
		return fmt.Errorf("delete address: %w", err)
	}
	return nil
}
