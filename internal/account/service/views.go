package service

import (
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
)

// AccountView is the Account schema in api/schemas/account.yaml.
type AccountView struct {
	ID          uuid.UUID     `json:"id"`
	DisplayName string        `json:"display_name"`
	Bio         string        `json:"bio"`
	AvatarURL   string        `json:"avatar_url,omitempty"`
	Status      domain.Status `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	// AwayUntil is set only while away mode is in force.
	AwayUntil *time.Time `json:"away_until,omitempty"`
	// LastActiveAt is not in the Account schema; views that show whether a
	// person is around copy it.
	LastActiveAt *time.Time `json:"-"`
}

// view is a's AccountView, the avatar's URL built from its key.
func (s *Account) view(a domain.Account, now time.Time) AccountView {
	v := AccountView{
		ID: a.ID(), DisplayName: a.DisplayName(), Bio: a.Bio(), Status: a.Status(now), CreatedAt: a.CreatedAt(),
		AwayUntil: a.AwayUntil(now), LastActiveAt: a.LastActiveAt(),
	}
	if a.AvatarKey() != "" {
		v.AvatarURL = s.d.Media.URL(a.AvatarKey())
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
	// AwayUntil turns away mode on until then; ClearAway turns it off.
	AwayUntil *time.Time
	ClearAway bool
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
	// Contact is the phone number, or the email address without one, so
	// console members can tell apart accounts that have no name.
	Contact   string    `json:"contact,omitempty"`
	CreatedAt time.Time `json:"created_at"`
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

// Proof is what a person shows to sign in, add an identity or
// reauthenticate. The spec's oneOf holds it to exactly one of a phone
// token, a Google ID token and an email code.
type Proof struct {
	PhoneToken  string     `json:"phone_token"`
	GoogleToken string     `json:"google_token"`
	EmailCode   *EmailCode `json:"email_code"`
}

// EmailCode is the code a person typed and the challenge it was mailed with.
type EmailCode struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Code        string    `json:"code"`
}

// SignInInput is a proof. A repeated Key with a proof of the same
// address gets the first request's account and session.
type SignInInput struct {
	Key        uuid.UUID
	Proof      Proof
	DeviceName string
	// Locale is the caller's language, which the account's mail follows;
	// empty when the caller names none.
	Locale locale.Tag
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

// Mailbox is an account's email address and the language of its mail.
type Mailbox struct {
	Address string     `json:"address"`
	Locale  locale.Tag `json:"locale"`
}

// SendEmailCodeInput is an address as the person typed it. Caller is the
// signed-in caller for an add-identity code and nil for a sign-in code.
type SendEmailCodeInput struct {
	Key    uuid.UUID
	Email  string
	Caller *authn.Principal
	IP     netip.Addr
	// Locale is the language the code's mail is written in.
	Locale locale.Tag
}

// EmailCodeView is the EmailCode schema in api/schemas/auth.yaml.
type EmailCodeView struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	ResendAt    time.Time `json:"resend_at"`
}

// AddIdentityInput is a proof of the kind the caller's account lacks.
type AddIdentityInput struct {
	Key    uuid.UUID
	Caller authn.Principal
	Proof  Proof
}

// ReauthenticateInput is a proof that must name an identity of the
// caller's account.
type ReauthenticateInput struct {
	Key    uuid.UUID
	Caller authn.Principal
	Proof  Proof
}

// IdentitiesView is the Identities schema in api/schemas/account.yaml.
type IdentitiesView struct {
	Identities []Identity `json:"identities"`
}

// MeView is the MyAccount schema: the account and its sign-in methods,
// which only its owner reads.
type MeView struct {
	AccountView

	Identities []Identity `json:"identities"`
}
