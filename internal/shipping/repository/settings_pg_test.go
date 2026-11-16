package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/shipping/domain"
	"github.com/shopnexus/server2/internal/shipping/repository"
)

func TestPickupSaveRacingAddressOrAccountDeletionLeavesNoPhone(t *testing.T) {
	t.Parallel()
	for _, erase := range []bool{false, true} {
		name := "address"
		if erase {
			name = "account"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db := pgtest.New(t)
			owner := accounttest.New(t, db)
			accounts := accounttest.Service(db)
			a, err := accounts.CreateAddress(ctx, owner, uuid.NewV7(), accountdomain.AddressDraft{
				Name: "Pickup", Phone: "0912345678", Street: "12 Lê Lợi", CommuneCode: "00004", Kind: accountdomain.AddressHome,
			})
			if err != nil {
				t.Fatal(err)
			}
			settings := repository.NewSettings(db)
			start := make(chan struct{})
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Go(func() {
				<-start
				errs <- settings.Put(ctx, owner, domain.Settings{Pickup: &domain.SavedPickup{AddressID: a.ID, Phone: "+84987654321"}})
			})
			wg.Go(func() {
				<-start
				if erase {
					errs <- accounts.Erase(ctx, owner)
				} else {
					errs <- accounts.DeleteAddress(ctx, owner, a.ID)
				}
			})
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil && !errors.Is(err, accountdomain.ErrAddressNotFound) && !errors.Is(err, accountdomain.ErrSessionRevoked) {
					t.Fatal(err)
				}
			}
			st, err := settings.Get(ctx, owner)
			if err != nil || st.Pickup != nil {
				t.Fatalf("preference survived deletion: %+v %v", st, err)
			}
			if erase {
				if err = settings.Put(ctx, owner, domain.Settings{}); !errors.Is(err, accountdomain.ErrSessionRevoked) {
					t.Fatalf("deleted account: %v", err)
				}
			}
		})
	}
}
