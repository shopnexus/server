// Package domain holds module auth's entities: an account's credentials,
// and sessions with the tokens signed for them. It sends nothing and stores
// nothing.
package domain

import (
	"slices"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/contact"
)

// Kind is the kind of an identity.
type Kind string

const KindPhone Kind = "phone"

// KindOf maps a contact kind to the identity kind of the same name.
func KindOf(a contact.Address) Kind { return Kind(a.Kind()) }

// IdentityRef names an identity and the account holding it.
type IdentityRef struct {
	AccountID  uuid.UUID `json:"account_id"`
	IdentityID uuid.UUID `json:"identity_id"`
}

// Identity is one verified way to sign in.
type Identity struct {
	id         uuid.UUID
	kind       Kind
	value      string
	lastUsedAt *time.Time
	createdAt  time.Time
}

// NewIdentity builds the identity of a phone number Firebase just verified;
// it counts as used at now.
func NewIdentity(id uuid.UUID, a contact.Address, now time.Time) Identity {
	return Identity{id: id, kind: KindOf(a), value: a.Value(), lastUsedAt: &now, createdAt: now}
}

func (i Identity) ID() uuid.UUID          { return i.id }
func (i Identity) Kind() Kind             { return i.kind }
func (i Identity) Value() string          { return i.value }
func (i Identity) LastUsedAt() *time.Time { return i.lastUsedAt }
func (i Identity) CreatedAt() time.Time   { return i.createdAt }

// RestoreIdentity rebuilds a stored identity. Stores only.
func RestoreIdentity(id uuid.UUID, kind Kind, value string, lastUsedAt *time.Time, createdAt time.Time) Identity {
	return Identity{id: id, kind: kind, value: value, lastUsedAt: lastUsedAt, createdAt: createdAt}
}

// Credentials is the aggregate of an account's sign-in methods: its verified
// identities and, from plan 3, its password.
type Credentials struct {
	accountID  uuid.UUID
	identities []Identity
	writeID    uuid.UUID
	updatedAt  time.Time
}

// NewCredentials starts the credentials of a new account with its first identity. The
// first write ID is the account ID.
func NewCredentials(accountID uuid.UUID, first Identity, now time.Time) Credentials {
	return Credentials{accountID: accountID, identities: []Identity{first}, writeID: accountID, updatedAt: now}
}

func (c *Credentials) AccountID() uuid.UUID   { return c.accountID }
func (c *Credentials) Identities() []Identity { return slices.Clone(c.identities) }
func (c *Credentials) WriteID() uuid.UUID     { return c.writeID }
func (c *Credentials) UpdatedAt() time.Time   { return c.updatedAt }

// RestoreCredentials rebuilds stored credentials. Stores only.
func RestoreCredentials(accountID uuid.UUID, identities []Identity, writeID uuid.UUID, updatedAt time.Time) Credentials {
	return Credentials{accountID: accountID, identities: identities, writeID: writeID, updatedAt: updatedAt}
}

// Saved records that a store wrote c under writeID. Stores only.
func (c *Credentials) Saved(writeID uuid.UUID) { c.writeID = writeID }

// MarkUsed records a sign-in with the identity at now. An identity c does
// not hold is ErrUnknownIdentity.
func (c *Credentials) MarkUsed(identityID uuid.UUID, now time.Time) error {
	c.identities = slices.Clone(c.identities)
	for n, i := range c.identities {
		if i.id == identityID {
			c.identities[n].lastUsedAt = &now
			c.updatedAt = now
			return nil
		}
	}
	return ErrUnknownIdentity
}
