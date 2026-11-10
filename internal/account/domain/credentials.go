package domain

import (
	"encoding/json"
	"slices"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/contact"
)

// Kind is the kind of an identity.
type Kind string

const (
	KindPhone Kind = "phone"
	KindEmail Kind = "email"
)

// IdentityRef names an identity and the account holding it.
type IdentityRef struct {
	AccountID  uuid.UUID `json:"account_id"`
	IdentityID uuid.UUID `json:"identity_id"`
}

// Identity is one verified way to sign in: a phone number or an email
// address.
type Identity struct {
	id         uuid.UUID
	kind       Kind
	value      string
	lastUsedAt *time.Time
	createdAt  time.Time
}

// NewIdentity builds the identity of an address just proven: a phone number
// Firebase verified, an email a code or Google proved. It counts as used at
// now. Its kind is the contact kind of the same name.
func NewIdentity(id uuid.UUID, a contact.Address, now time.Time) Identity {
	return Identity{id: id, kind: Kind(a.Kind()), value: a.Value(), lastUsedAt: &now, createdAt: now}
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

// Credentials is the aggregate of an account's sign-in methods: at least one
// identity, at most one of each kind.
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

// Has reports whether c holds an identity of kind k.
func (c *Credentials) Has(k Kind) bool {
	return slices.ContainsFunc(c.identities, func(i Identity) bool { return i.kind == k })
}

// Add gives c the identity i, of a kind c lacks; a kind c holds is
// ErrIdentityKindTaken and changes nothing. A value another account holds
// surfaces from the store's Save as ErrIdentityTaken.
func (c *Credentials) Add(i Identity) error {
	if c.Has(i.kind) {
		return ErrIdentityKindTaken
	}
	c.identities = append(slices.Clone(c.identities), i)
	c.updatedAt = i.createdAt
	return nil
}

// identityJSON and credentialsJSON are how a Restate step journals
// credentials it loaded.
type identityJSON struct {
	ID         uuid.UUID  `json:"id"`
	Kind       Kind       `json:"kind"`
	Value      string     `json:"value"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type credentialsJSON struct {
	AccountID  uuid.UUID      `json:"account_id"`
	Identities []identityJSON `json:"identities"`
	WriteID    uuid.UUID      `json:"write_id"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

func (c Credentials) MarshalJSON() ([]byte, error) {
	j := credentialsJSON{AccountID: c.accountID, Identities: make([]identityJSON, 0, len(c.identities)), WriteID: c.writeID, UpdatedAt: c.updatedAt}
	for _, i := range c.identities {
		j.Identities = append(j.Identities, identityJSON{i.id, i.kind, i.value, i.lastUsedAt, i.createdAt})
	}
	return json.Marshal(j)
}

func (c *Credentials) UnmarshalJSON(b []byte) error {
	var j credentialsJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	ids := make([]Identity, 0, len(j.Identities))
	for _, i := range j.Identities {
		ids = append(ids, RestoreIdentity(i.ID, i.Kind, i.Value, i.LastUsedAt, i.CreatedAt))
	}
	*c = RestoreCredentials(j.AccountID, ids, j.WriteID, j.UpdatedAt)
	return nil
}
