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
}

// view is a's AccountView, the avatar's URL built from its key.
func (s *Account) view(a domain.Account, now time.Time) AccountView {
	v := AccountView{ID: a.ID(), DisplayName: a.DisplayName(), Bio: a.Bio(), Status: a.Status(now), CreatedAt: a.CreatedAt()}
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

// Record is an account as staff see it: the StaffAccount schema in
// api/schemas/staff.yaml. A suspension that has lapsed reads as active, with
// no reason or end.
type Record struct {
	ID             uuid.UUID     `json:"id"`
	DisplayName    string        `json:"display_name"`
	Status         domain.Status `json:"status"`
	SuspendedUntil *time.Time    `json:"suspended_until,omitempty"`
	SuspendReason  string        `json:"suspend_reason,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
}

func recordOf(a domain.Account, now time.Time) Record {
	r := Record{ID: a.ID(), DisplayName: a.DisplayName(), Status: a.Status(now), CreatedAt: a.CreatedAt()}
	if r.Status == domain.StatusSuspended {
		r.SuspendedUntil, r.SuspendReason = a.SuspendedUntil(), a.SuspendReason()
	}
	return r
}

// SuspendInput suspends account ID until Until or, with a nil Until, until
// Reinstate. By is recorded as the actor.
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
