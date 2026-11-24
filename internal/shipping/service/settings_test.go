package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/domain"
	"github.com/shopnexus/server/internal/shipping/repository"
	"github.com/shopnexus/server/internal/shipping/service"
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

// spy records the pickup of each shipment it makes.
type spy struct {
	carrier.Fake

	from []carrier.Pickup
}

func (s *spy) Create(ctx context.Context, a carrier.Account, in carrier.CreateInput) (carrier.Created, error) {
	s.from = append(s.from, in.Pickup)
	return s.Fake.Create(ctx, a, in)
}

// A shop joined by a code ships from the saved pickup when none is asked.
func TestAJoinedShopShipsFromTheSavedPickup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	owner := accounttest.New(t, db)
	accounts := accounttest.Service(db)
	ghn := &spy{Carrier: carrier.GHN}
	svc := service.New(service.Deps{
		Customers: accounts, Settings: repository.NewSettings(db), Accounts: repository.NewAccounts(db),
		Clients: map[carrier.Carrier]carrier.Client{carrier.GHN: ghn}, Key: make([]byte, 32), Now: time.Now,
	})
	if _, err := svc.Link(ctx, owner, "ghn", carrier.Credentials{Phone: "0356875408", OTP: "860228"}); err != nil {
		t.Fatal(err)
	}
	in := carrier.CreateInput{OrderCode: "SN-1", Item: "Ghế", WeightGrams: 500, Service: "standard", Option: "home"}
	if _, err := svc.Create(ctx, owner, carrier.GHN, in); !errors.Is(err, domain.ErrPickupNotConfigured) {
		t.Fatalf("no saved pickup: %v", err)
	}
	draft := accountdomain.AddressDraft{Name: "Tú", Phone: "0912345678", Street: "12 Lê Lợi", CommuneCode: "00004", Kind: accountdomain.AddressHome}
	a, err := accounts.CreateAddress(ctx, owner, uuid.NewV7(), draft, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PutSettings(ctx, owner, domain.Settings{Pickup: &domain.SavedPickup{AddressID: a.ID, Phone: "0987654321"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Create(ctx, owner, carrier.GHN, in); err != nil {
		t.Fatal(err)
	}
	if len(ghn.from) != 1 || ghn.from[0].Place == nil || ghn.from[0].Place.Street != "12 Lê Lợi" || ghn.from[0].Place.Phone != "+84987654321" {
		t.Fatalf("%+v", ghn.from)
	}
}
