// Package credentials holds the aggregate of an account's sign-in methods:
// its verified identities and, from plan 3, its password.
package credentials

import (
	"errors"
	"slices"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
)

type Kind string

const (
	Phone  Kind = "phone"
	Email  Kind = "email"
	Google Kind = "google"
)

var (
	ErrNotFound = errors.New("credentials: not found")
	ErrConflict = errors.New("credentials: changed meanwhile")
	ErrTaken    = apperr.New(apperr.KindConflict, "identity_taken", "this sign-in method belongs to another account")
)

// KindOf maps a contact kind to the identity kind of the same name.
func KindOf(a contact.Address) Kind { return Kind(a.Kind()) }

// Identity is one verified way to sign in.
type Identity struct {
	id         uuid.UUID
	kind       Kind
	value      string
	lastUsedAt *time.Time
	createdAt  time.Time
}

// NewIdentity builds the identity for a phone or email whose code was just
// verified; it counts as used at now.
func NewIdentity(id uuid.UUID, a contact.Address, now time.Time) Identity {
	return Identity{id: id, kind: KindOf(a), value: a.Value(), lastUsedAt: &now, createdAt: now}
}

// RestoreIdentity rebuilds a stored identity. Stores only.
func RestoreIdentity(id uuid.UUID, kind Kind, value string, lastUsedAt *time.Time, createdAt time.Time) Identity {
	return Identity{id, kind, value, lastUsedAt, createdAt}
}

func (i Identity) ID() uuid.UUID          { return i.id }
func (i Identity) Kind() Kind             { return i.kind }
func (i Identity) Value() string          { return i.value }
func (i Identity) LastUsedAt() *time.Time { return i.lastUsedAt }
func (i Identity) CreatedAt() time.Time   { return i.createdAt }

// Credentials is every sign-in method of one account.
type Credentials struct {
	accountID  uuid.UUID
	identities []Identity
	writeID    uuid.UUID
	updatedAt  time.Time
}

// New starts the credentials of a new account with its first identity. The
// first write ID is the account ID.
func New(accountID uuid.UUID, first Identity, now time.Time) Credentials {
	return Credentials{accountID: accountID, identities: []Identity{first}, writeID: accountID, updatedAt: now}
}

// Restore rebuilds stored credentials. Stores only.
func Restore(accountID uuid.UUID, identities []Identity, writeID uuid.UUID, updatedAt time.Time) Credentials {
	return Credentials{accountID, identities, writeID, updatedAt}
}

func (c Credentials) AccountID() uuid.UUID   { return c.accountID }
func (c Credentials) Identities() []Identity { return slices.Clone(c.identities) }
func (c Credentials) WriteID() uuid.UUID     { return c.writeID }
func (c Credentials) UpdatedAt() time.Time   { return c.updatedAt }

// MarkUsed records a sign-in with the identity at now.
func (c *Credentials) MarkUsed(identityID uuid.UUID, now time.Time) error {
	c.identities = slices.Clone(c.identities)
	for n, i := range c.identities {
		if i.id == identityID {
			c.identities[n].lastUsedAt = &now
			c.updatedAt = now
			return nil
		}
	}
	return ErrNotFound
}
