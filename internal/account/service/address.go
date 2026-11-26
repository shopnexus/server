package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// AddressView is the Address schema in api/schemas/address.yaml.
type AddressView struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Phone       string    `json:"phone"`
	Street      string    `json:"street"`
	CommuneCode string    `json:"commune_code"`
	// ProvinceCode is the commune's province, for a form that picks the
	// province first.
	ProvinceCode string             `json:"province_code"`
	Commune      string             `json:"commune"`
	Province     string             `json:"province"`
	Kind         domain.AddressKind `json:"kind"`
	IsDefault    bool               `json:"is_default"`
	CreatedAt    time.Time          `json:"created_at"`
}

func addressView(a domain.Address, p repository.Place) AddressView {
	return AddressView{
		ID: a.ID(), Name: a.Name(), Phone: a.Phone(), Street: a.Street(), CommuneCode: a.CommuneCode(), ProvinceCode: p.ProvinceCode,
		Commune: p.Commune, Province: p.Province, Kind: a.Kind(), IsDefault: a.IsDefault(), CreatedAt: a.CreatedAt(),
	}
}

// commune reads the place of code; found is false for an unknown code.
func (s *Account) commune(ctx context.Context, code string) (place repository.Place, found bool, err error) {
	place, err = s.d.Addresses.Commune(ctx, code)
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
	a, err := s.d.Addresses.Get(ctx, owner, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Address{}, domain.ErrAddressNotFound
	}
	if err != nil {
		return domain.Address{}, fmt.Errorf("get address: %w", err)
	}
	return a, nil
}

// Addresses is UC-ACC-07, step 1.
func (s *Account) Addresses(ctx context.Context, owner uuid.UUID) ([]AddressView, error) {
	list, err := s.d.Addresses.List(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("list addresses: %w", err)
	}
	out := make([]AddressView, 0, len(list))
	for i := range list {
		out = append(out, addressView(list[i].Address, list[i].Place))
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

// CreateAddress is UC-ACC-07, steps 2 and 3.
func (s *Account) CreateAddress(ctx context.Context, owner, key uuid.UUID, d domain.AddressDraft, makeDefault bool) (AddressView, error) {
	place, found, err := s.commune(ctx, d.CommuneCode)
	if err != nil {
		return AddressView{}, err
	}
	id := uuid.NewV7()
	// E1
	a, err := domain.NewAddress(id, owner, d, found, s.d.Now())
	if err != nil {
		return AddressView{}, err
	}
	// 3. Save; the first address is the default. E2 at the cap; A2 as the default.
	if err = s.d.Addresses.Insert(ctx, key, &a, makeDefault); err != nil {
		return AddressView{}, fmt.Errorf("insert address: %w", err)
	}
	// A4 · NFR-IDEM-01
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
// With makeDefault the save also makes the address the default.
//
// UpdateAddress is UC-ACC-07 A1 and A2.
func (s *Account) UpdateAddress(ctx context.Context, owner, id uuid.UUID, d domain.AddressDraft, makeDefault bool) (AddressView, error) {
	// E3 · BR-ACC-02
	a, err := s.owned(ctx, owner, id)
	if err != nil {
		return AddressView{}, err
	}
	place, found, err := s.commune(ctx, d.CommuneCode)
	if err != nil {
		return AddressView{}, err
	}
	// E1
	if err = a.Edit(d, found); err != nil {
		return AddressView{}, err
	}
	// E4 · NFR-CONC-01
	err = s.d.Addresses.Save(ctx, &a, uuid.NewV7(), makeDefault)
	if errors.Is(err, repository.ErrConflict) {
		return AddressView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return AddressView{}, fmt.Errorf("save address: %w", err)
	}
	return addressView(a, place), nil
}

// SetDefaultAddress is UC-ACC-07 A2.
func (s *Account) SetDefaultAddress(ctx context.Context, owner, id uuid.UUID) error {
	err := s.d.Addresses.SetDefault(ctx, owner, id, uuid.NewV7())
	// E3 · BR-ACC-02
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrAddressNotFound
	}
	if err != nil {
		return fmt.Errorf("set default address: %w", err)
	}
	return nil
}

// DeleteAddress is UC-ACC-07 A3.
func (s *Account) DeleteAddress(ctx context.Context, owner, id uuid.UUID) error {
	err := s.d.Addresses.Delete(ctx, owner, id, uuid.NewV7())
	// E3 · BR-ACC-02
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrAddressNotFound
	}
	if err != nil {
		return fmt.Errorf("delete address: %w", err)
	}
	return nil
}
