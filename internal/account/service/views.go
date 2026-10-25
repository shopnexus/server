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
	Status      domain.Status `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
}

func viewOf(a domain.Account, now time.Time) AccountView {
	return AccountView{ID: a.ID(), DisplayName: a.DisplayName(), Status: a.Status(now), CreatedAt: a.CreatedAt()}
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
