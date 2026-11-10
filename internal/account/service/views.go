package service

import (
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/audit"
)

// AccountView is the Account schema in api/schemas/account.yaml.
type AccountView struct {
	ID          uuid.UUID     `json:"id"`
	DisplayName string        `json:"display_name"`
	Bio         string        `json:"bio"`
	AvatarURL   string        `json:"avatar_url,omitempty"`
	Status      domain.Status `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	// LastActiveAt is not in the Account schema; views that show whether a
	// person is around copy it.
	LastActiveAt *time.Time `json:"-"`
}

// view is a's AccountView, the avatar's URL built from its key.
func (s *Account) view(a domain.Account, now time.Time) AccountView {
	v := AccountView{ID: a.ID(), DisplayName: a.DisplayName(), Bio: a.Bio(), Status: a.Status(now), CreatedAt: a.CreatedAt(), LastActiveAt: a.LastActiveAt()}
	if a.AvatarKey() != "" {
		v.AvatarURL = s.media.URL(a.AvatarKey())
	}
	return v
}

// UpdateInput is what Update changes; a nil field stays as it is.
type UpdateInput struct {
	DisplayName *string
	Bio         *string
	// Avatar is an upload of the account's own, to be its avatar.
	Avatar *uuid.UUID
	// RemoveAvatar takes the avatar away; Avatar is then nil.
	RemoveAvatar bool
}

// ConsoleAccount is an account as console members see it: the ConsoleAccount
// schema in api/schemas/console.yaml. A suspension that has lapsed reads as active, with
// no reason or end.
type ConsoleAccount struct {
	ID             uuid.UUID     `json:"id"`
	DisplayName    string        `json:"display_name"`
	Status         domain.Status `json:"status"`
	SuspendedUntil *time.Time    `json:"suspended_until,omitempty"`
	SuspendReason  string        `json:"suspend_reason,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
}

func consoleAccountOf(a domain.Account, now time.Time) ConsoleAccount {
	r := ConsoleAccount{ID: a.ID(), DisplayName: a.DisplayName(), Status: a.Status(now), CreatedAt: a.CreatedAt()}
	if r.Status == domain.StatusSuspended {
		r.SuspendedUntil, r.SuspendReason = a.SuspendedUntil(), a.SuspendReason()
	}
	return r
}

// SuspendInput suspends account ID until Until or, with a nil Until, until
// Unsuspend. By is recorded as the actor.
type SuspendInput struct {
	ID     uuid.UUID
	Reason string
	Until  *time.Time
	By     audit.Actor
}

// BlockView is the Block schema in api/schemas/account.yaml.
type BlockView struct {
	ID          uuid.UUID `json:"id"`
	AccountID   uuid.UUID `json:"account_id"`
	DisplayName string    `json:"display_name"`
	At          time.Time `json:"at"`
}

// FollowView is the Follow schema in api/schemas/account.yaml.
type FollowView struct {
	ID          uuid.UUID `json:"id"`
	AccountID   uuid.UUID `json:"account_id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	At          time.Time `json:"at"`
}

// Follower is one follow of an account: the follow's ID, which pages, and
// the follower. Notification reads them to tell of a new listing.
type Follower struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
}

// SignInInput is a phone token from Firebase. A repeated IdempotencyKey with
// a token for the same number gets the first request's account.
type SignInInput struct {
	IdempotencyKey string
	PhoneToken     string
	DeviceName     string
}

// SignedIn is the SignIn response in api/schemas/auth.yaml.
type SignedIn struct {
	domain.Tokens

	Account SignedInAccount `json:"account"`
}

type SignedInAccount struct {
	ID    uuid.UUID `json:"id"`
	IsNew bool      `json:"is_new"`
}

// SessionItem is the Session schema in api/schemas/auth.yaml: one device in
// the caller's list.
type SessionItem struct {
	ID         uuid.UUID `json:"id"`
	DeviceName string    `json:"device_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

// Identity is a sign-in method as console members see it: the Identity schema in
// api/schemas/console.yaml.
type Identity struct {
	Kind  domain.Kind `json:"kind"`
	Value string      `json:"value"`
}
