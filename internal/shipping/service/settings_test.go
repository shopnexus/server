package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/shipping/domain"
	"github.com/shopnexus/server2/internal/shipping/repository"
	"github.com/shopnexus/server2/internal/shipping/service"
)

func TestSavedPickupFollowsAddressAndClearsPhoneOnDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	owner, other := accounttest.New(t, db), accounttest.New(t, db)
	accounts := accounttest.Service(db)
	svc := service.New(service.Deps{Customers: accounts, Settings: repository.NewSettings(db)})
	draft := accountdomain.AddressDraft{Name: "Book name", Phone: "0912345678", Street: "12 Lê Lợi", CommuneCode: "00004", Kind: accountdomain.AddressHome}
	a, err := accounts.CreateAddress(ctx, owner, uuid.NewV7(), draft, false)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := accounts.CreateAddress(ctx, other, uuid.NewV7(), draft, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{foreign.ID, uuid.NewV7()} {
		if _, err = svc.PutSettings(ctx, owner, domain.Settings{Pickup: &domain.SavedPickup{AddressID: id, Phone: "0987654321"}}); !errors.Is(err, accountdomain.ErrAddressNotFound) {
			t.Fatalf("foreign or missing: %v", err)
		}
	}
	if _, err = svc.PutSettings(ctx, owner, domain.Settings{Pickup: &domain.SavedPickup{AddressID: a.ID, Phone: "123"}}); !errors.Is(err, domain.ErrInvalidSettings) {
		t.Fatalf("phone: %v", err)
	}
	st, err := svc.PutSettings(ctx, owner, domain.Settings{Pickup: &domain.SavedPickup{AddressID: a.ID, Phone: "0987654321"}})
	if err != nil || st.Pickup.Phone != "+84987654321" {
		t.Fatalf("settings: %+v %v", st, err)
	}
	draft.Street = "15 Lê Lợi"
	if _, err = accounts.UpdateAddress(ctx, owner, a.ID, draft, false); err != nil {
		t.Fatal(err)
	}
	p, err := svc.SavedPickup(ctx, owner)
	if err != nil || p.Street != draft.Street || p.Phone != st.Pickup.Phone || p.Name != "Linh" || p.Commune == "" {
		t.Fatalf("resolved: %+v %v", p, err)
	}
	if err = accounts.DeleteAddress(ctx, owner, a.ID); err != nil {
		t.Fatal(err)
	}
	if st, err = svc.Settings(ctx, owner); err != nil || st.Pickup != nil {
		t.Fatalf("deleted settings: %+v %v", st, err)
	}
	if _, err = svc.SavedPickup(ctx, owner); !errors.Is(err, domain.ErrPickupNotConfigured) {
		t.Fatalf("unconfigured: %v", err)
	}
	if _, err = svc.PutSettings(ctx, owner, domain.Settings{}); err != nil {
		t.Fatal(err)
	}
}
