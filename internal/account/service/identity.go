package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/contact"
)

// FindByPhone returns the account holding the phone number q, in any form a
// person types. A q that is no Vietnamese mobile number, or that no account
// holds, reports false.
func (s *Account) FindByPhone(ctx context.Context, q string) (uuid.UUID, bool, error) {
	addr, err := contact.ParsePhone(q)
	if err != nil {
		return uuid.UUID{}, false, nil //nolint:nilerr // A q that is no phone number matches no account.
	}
	ref, err := s.credentials.FindIdentity(ctx, domain.KindPhone, addr.Value())
	if errors.Is(err, repository.ErrNotFound) {
		return uuid.UUID{}, false, nil
	}
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("find identity: %w", err)
	}
	return ref.AccountID, true, nil
}

// Identities lists an account's sign-in methods; an account without
// credentials has none.
func (s *Account) Identities(ctx context.Context, accountID uuid.UUID) ([]Identity, error) {
	cr, err := s.credentials.Get(ctx, accountID)
	if errors.Is(err, repository.ErrNotFound) {
		return []Identity{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get credentials: %w", err)
	}
	ids := cr.Identities()
	out := make([]Identity, 0, len(ids))
	for _, i := range ids {
		out = append(out, Identity{Kind: i.Kind(), Value: i.Value()})
	}
	return out, nil
}

// HasPhone reports whether the account signs in with a phone number, which
// posting, opening a deal and opening a chat need. An account without
// credentials has none.
func (s *Account) HasPhone(ctx context.Context, id uuid.UUID) (bool, error) {
	cr, err := s.credentials.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get credentials: %w", err)
	}
	return cr.Has(domain.KindPhone), nil
}
