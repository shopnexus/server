package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/erasure/service"
	"github.com/shopnexus/server2/internal/platform/authn"
)

var now = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

// accounts is an account service that records Erase and answers err.
type accounts struct {
	accountsvc.Service

	erased *[]uuid.UUID
	err    error
}

func (a accounts) Erase(_ context.Context, id uuid.UUID) error {
	*a.erased = append(*a.erased, id)
	return a.err
}

// deals is a deal service whose OpenOrders answers open.
type deals struct {
	dealsvc.Service

	open bool
}

func (d deals) OpenOrders(context.Context, uuid.UUID) (bool, error) { return d.open, nil }

func TestDeleteAccount(t *testing.T) {
	t.Parallel()
	me := uuid.NewV7()
	fresh := authn.Principal{AccountID: me, AuthTime: now.Add(-14 * time.Minute)}
	stale := authn.Principal{AccountID: me, AuthTime: now.Add(-16 * time.Minute)}
	for _, tc := range []struct {
		name     string
		p        authn.Principal
		open     bool
		eraseErr error
		want     error
		erased   int
	}{
		{"deleted", fresh, false, nil, nil, 1},
		{"stale sign-in", stale, false, nil, accountdomain.ErrReauthRequired, 0},
		{"open order", fresh, true, nil, dealdomain.ErrOrdersOpen, 0},
		{"suspended", fresh, false, accountdomain.ErrSuspended, accountdomain.ErrSuspended, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var erased []uuid.UUID
			svc := service.New(service.Deps{
				Accounts: accounts{erased: &erased, err: tc.eraseErr}, Deals: deals{open: tc.open},
				Now: func() time.Time { return now },
			})
			err := svc.DeleteAccount(context.Background(), tc.p)
			if (tc.want == nil && err != nil) || (tc.want != nil && !errors.Is(err, tc.want)) || len(erased) != tc.erased {
				t.Fatalf("err %v, erased %v", err, erased)
			}
		})
	}
}
