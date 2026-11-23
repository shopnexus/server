package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/contact"
)

// FindByContact returns the account holding the email address or the phone
// number q, in any form a person types. A q that is neither, or that no
// account holds, reports false.
func (s *Account) FindByContact(ctx context.Context, q string) (uuid.UUID, bool, error) {
	addr, err := contact.ParseEmail(q)
	if err != nil {
		if addr, err = contact.ParsePhone(q); err != nil {
			return uuid.UUID{}, false, nil //nolint:nilerr // A q that is no address matches no account.
		}
	}
	ref, err := s.credentials.FindIdentity(ctx, domain.Kind(addr.Kind()), addr.Value())
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

func (s *Account) Locale(ctx context.Context, id uuid.UUID) (locale.Tag, error) {
	tag, err := s.accounts.Locale(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read locale: %w", err)
	}
	return locale.Of(tag), nil
}

func (s *Account) Mailbox(ctx context.Context, id uuid.UUID) (Mailbox, error) {
	addr, tag, err := s.accounts.Mailbox(ctx, id)
	if err != nil {
		return Mailbox{}, fmt.Errorf("read mailbox: %w", err)
	}
	return Mailbox{Address: addr, Locale: locale.Of(tag)}, nil
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
