package account

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
)

// Record is an account as staff see it: the StaffAccount schema in
// api/schemas/staff.yaml. A suspension that has lapsed reads as active, with
// no reason or end.
type Record struct {
	ID             uuid.UUID  `json:"id"`
	DisplayName    string     `json:"display_name"`
	Status         Status     `json:"status"`
	SuspendedUntil *time.Time `json:"suspended_until,omitempty"`
	SuspendReason  string     `json:"suspend_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

func recordOf(a Account, now time.Time) Record {
	r := Record{ID: a.ID(), DisplayName: a.DisplayName(), Status: a.Status(now), CreatedAt: a.CreatedAt()}
	if r.Status == StatusSuspended {
		r.SuspendedUntil, r.SuspendReason = a.SuspendedUntil(), a.SuspendReason()
	}
	return r
}

// GetRecord reads one account as staff see it.
func GetRecord(ctx context.Context, accounts Store, id uuid.UUID, now time.Time) (Record, error) {
	a, err := accounts.Get(ctx, id)
	if err != nil {
		return Record{}, fmt.Errorf("get account: %w", err)
	}
	return recordOf(a, now), nil
}

// Records lists accounts newest first. It returns the p.Fetch() rows the
// store read; the caller cuts the page.
func Records(ctx context.Context, accounts Store, p page.Request, now time.Time) ([]Record, error) {
	list, err := accounts.List(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	out := make([]Record, 0, len(list))
	for i := range list {
		out = append(out, recordOf(list[i], now))
	}
	return out, nil
}
