package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/platform/contact"
)

// Identity is a sign-in method as staff see it: the Identity schema in
// api/schemas/staff.yaml.
type Identity struct {
	Kind  credentials.Kind `json:"kind"`
	Value string           `json:"value"`
}

// Client implements the ports other modules declare for auth's data. It
// only reads.
type Client struct {
	Credentials credentials.Store
}

// FindAccount returns the account holding the phone number or email q, in
// any form a person types. A q that is neither, or that no account holds,
// reports false.
func (c Client) FindAccount(ctx context.Context, q string) (uuid.UUID, bool, error) {
	var (
		a   contact.Address
		err error
	)
	if strings.Contains(q, "@") {
		a, err = contact.ParseEmail(q)
	} else {
		a, err = contact.ParsePhone(q)
	}
	if err != nil {
		return uuid.UUID{}, false, nil //nolint:nilerr // A q that is no phone or email matches no account.
	}
	ref, err := c.Credentials.FindIdentity(ctx, credentials.KindOf(a), a.Value())
	if errors.Is(err, credentials.ErrNotFound) {
		return uuid.UUID{}, false, nil
	}
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("find identity: %w", err)
	}
	return ref.AccountID, true, nil
}

// Identities lists an account's sign-in methods; an account without
// credentials has none.
func (c Client) Identities(ctx context.Context, accountID uuid.UUID) ([]Identity, error) {
	cr, err := c.Credentials.Get(ctx, accountID)
	if errors.Is(err, credentials.ErrNotFound) {
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
