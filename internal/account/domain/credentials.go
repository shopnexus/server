package domain

import (
	"encoding/json"
	"slices"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/contact"
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
type Identity struct{ s IdentityState }

// IdentityState is every stored field of an identity. Stores only.
type IdentityState struct {
	ID         uuid.UUID  `json:"id"`
	Kind       Kind       `json:"kind"`
	Value      string     `json:"value"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NewIdentity builds the identity of an address just proven: a phone number
// Firebase verified, an email a code or Google proved. It counts as used at
// now. Its kind is the contact kind of the same name.
func NewIdentity(id uuid.UUID, a contact.Address, now time.Time) Identity {
	return Identity{s: IdentityState{ID: id, Kind: Kind(a.Kind()), Value: a.Value(), LastUsedAt: &now, CreatedAt: now}}
}

func (i Identity) ID() uuid.UUID          { return i.s.ID }
func (i Identity) Kind() Kind             { return i.s.Kind }
func (i Identity) Value() string          { return i.s.Value }
func (i Identity) LastUsedAt() *time.Time { return i.s.LastUsedAt }
func (i Identity) CreatedAt() time.Time   { return i.s.CreatedAt }

// RestoreIdentity rebuilds a stored identity. Stores only.
func RestoreIdentity(st IdentityState) Identity { return Identity{s: st} }

// Credentials is the aggregate of an account's sign-in methods: at least one
// identity, at most one of each kind.
type Credentials struct{ s CredentialsState }

// CredentialsState is every stored field of credentials, and how a Restate
// step journals credentials it loaded. Stores only.
type CredentialsState struct {
	AccountID  uuid.UUID       `json:"account_id"`
	Identities []IdentityState `json:"identities"`
	WriteID    uuid.UUID       `json:"write_id"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// NewCredentials starts the credentials of a new account with its first identity. The
// first write ID is the account ID.
func NewCredentials(accountID uuid.UUID, first Identity, now time.Time) Credentials {
	return Credentials{s: CredentialsState{AccountID: accountID, Identities: []IdentityState{first.s}, WriteID: accountID, UpdatedAt: now}}
}

func (c *Credentials) AccountID() uuid.UUID { return c.s.AccountID }

func (c *Credentials) Identities() []Identity {
	out := make([]Identity, len(c.s.Identities))
	for n, st := range c.s.Identities {
		out[n] = Identity{s: st}
	}
	return out
}

func (c *Credentials) WriteID() uuid.UUID   { return c.s.WriteID }
func (c *Credentials) UpdatedAt() time.Time { return c.s.UpdatedAt }

// RestoreCredentials rebuilds stored credentials. Stores only.
func RestoreCredentials(st CredentialsState) Credentials { return Credentials{s: st} }

// Saved records that a store wrote c under writeID. Stores only.
func (c *Credentials) Saved(writeID uuid.UUID) { c.s.WriteID = writeID }

// MarkUsed records a sign-in with the identity at now. An identity c does
// not hold is ErrUnknownIdentity.
func (c *Credentials) MarkUsed(identityID uuid.UUID, now time.Time) error {
	c.s.Identities = slices.Clone(c.s.Identities)
	for n, i := range c.s.Identities {
		if i.ID == identityID {
			c.s.Identities[n].LastUsedAt = &now
			c.s.UpdatedAt = now
			return nil
		}
	}
	return ErrUnknownIdentity
}

// Has reports whether c holds an identity of kind k.
func (c *Credentials) Has(k Kind) bool {
	return slices.ContainsFunc(c.s.Identities, func(i IdentityState) bool { return i.Kind == k })
}

// Add gives c the identity i, of a kind c lacks; a kind c holds is
// ErrIdentityKindTaken and changes nothing. A value another account holds
// surfaces from the store's Save as ErrIdentityTaken.
func (c *Credentials) Add(i Identity) error {
	if c.Has(i.s.Kind) {
		return ErrIdentityKindTaken
	}
	c.s.Identities = append(slices.Clone(c.s.Identities), i.s)
	c.s.UpdatedAt = i.s.CreatedAt
	return nil
}

// MarshalJSON journals no identities as [], never null.
func (c Credentials) MarshalJSON() ([]byte, error) {
	st := c.s
	if st.Identities == nil {
		st.Identities = []IdentityState{}
	}
	return json.Marshal(st)
}

func (c *Credentials) UnmarshalJSON(b []byte) error {
	var st CredentialsState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	*c = Credentials{s: st}
	return nil
}
