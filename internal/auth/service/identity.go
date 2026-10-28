package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/contact"
)

// FindAccount returns the account holding the phone number q, in any form a
// person types. A q that is no Vietnamese mobile number, or that no account
// holds, reports false.
func (a *Auth) FindAccount(ctx context.Context, q string) (uuid.UUID, bool, error) {
	addr, err := contact.ParsePhone(q)
	if err != nil {
		return uuid.UUID{}, false, nil //nolint:nilerr // A q that is no phone number matches no account.
	}
	ref, err := a.credentials.FindIdentity(ctx, domain.KindPhone, addr.Value())
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
func (a *Auth) Identities(ctx context.Context, accountID uuid.UUID) ([]Identity, error) {
	cr, err := a.credentials.Get(ctx, accountID)
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
