package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

var home = domain.AddressDraft{Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004"}

func TestAddressBook(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	svc := at(s, now)
	me, other := seed(t, s), seed(t, s)

	first, err := svc.CreateAddress(ctx, me, uuid.NewV7(), home)
	if err != nil || !first.IsDefault || first.Phone != "+84912345678" || first.Commune == "" || first.Province == "" ||
		!first.CreatedAt.Equal(now) {
		t.Fatalf("%+v %v", first, err)
	}
	key := uuid.NewV7()
	second, err := svc.CreateAddress(ctx, me, key, domain.AddressDraft{Name: "Minh", Phone: "0987654321", Street: "1 Lê Lợi", CommuneCode: "00004"})
	if err != nil || second.IsDefault {
		t.Fatalf("%+v %v", second, err)
	}
	again, err := svc.CreateAddress(ctx, me, key, domain.AddressDraft{Name: "Minh", Phone: "0987654321", Street: "1 Lê Lợi", CommuneCode: "00004"})
	if err != nil || again.ID != second.ID || again.Name != second.Name || again.IsDefault ||
		!again.CreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("a retried request: %+v %v", again, err)
	}
	got, err := svc.Address(ctx, me, second.ID)
	if err != nil || got.ID != second.ID || got.Name != "Minh" || got.Commune != second.Commune || got.IsDefault ||
		!got.CreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("%+v %v", got, err)
	}

	edited, err := svc.UpdateAddress(ctx, me, first.ID, domain.AddressDraft{Name: "Lan", Phone: "0901234567", Street: "3 Hàng Bài", CommuneCode: "00004"})
	if err != nil || edited.Name != "Lan" || edited.Phone != "+84901234567" || !edited.IsDefault || edited.Commune == "" {
		t.Fatalf("%+v %v", edited, err)
	}
	if err = svc.SetDefaultAddress(ctx, me, second.ID); err != nil {
		t.Fatal(err)
	}
	list, err := svc.Addresses(ctx, me)
	if err != nil || len(list) != 2 || list[0].ID != second.ID || !list[0].IsDefault || list[1].IsDefault || list[1].Name != "Lan" {
		t.Fatalf("%+v %v", list, err)
	}
	if err = svc.DeleteAddress(ctx, me, second.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ = svc.Addresses(ctx, me); len(list) != 1 || list[0].ID != first.ID || !list[0].IsDefault {
		t.Fatalf("after deleting the default: %+v", list)
	}

	for name, call := range map[string]func() error{
		"read":        func() error { _, err := svc.Address(ctx, other, first.ID); return err },
		"update":      func() error { _, err := svc.UpdateAddress(ctx, other, first.ID, home); return err },
		"set default": func() error { return svc.SetDefaultAddress(ctx, other, first.ID) },
		"delete":      func() error { return svc.DeleteAddress(ctx, other, first.ID) },
		"unknown":     func() error { _, err := svc.Address(ctx, me, uuid.NewV7()); return err },
	} {
		if err := call(); !errors.Is(err, domain.ErrAddressNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if list, _ = svc.Addresses(ctx, other); len(list) != 0 {
		t.Fatalf("someone else's book: %+v", list)
	}
}

func TestAddressRefusals(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	svc := at(s, now)
	me := seed(t, s)
	bad := home
	bad.CommuneCode = "nope"
	if _, err := svc.CreateAddress(ctx, me, uuid.NewV7(), bad); !errors.Is(err, domain.ErrInvalidAddress) {
		t.Errorf("an unknown commune: %v", err)
	}
	a, err := svc.CreateAddress(ctx, me, uuid.NewV7(), home)
	if err != nil {
		t.Fatal(err)
	}
	bad = home
	bad.Phone = "123"
	if _, err = svc.UpdateAddress(ctx, me, a.ID, bad); !errors.Is(err, domain.ErrInvalidAddress) {
		t.Errorf("a bad phone: %v", err)
	}
	for range domain.MaxAddresses - 1 {
		if _, err = svc.CreateAddress(ctx, me, uuid.NewV7(), home); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = svc.CreateAddress(ctx, me, uuid.NewV7(), home); !errors.Is(err, domain.ErrAddressLimit) {
		t.Errorf("the eleventh: %v", err)
	}
}

// staleAddress is a store whose GetAddress returns an address read before
// another write.
type staleAddress struct {
	repository.Accounts

	stale domain.Address
}

func (s staleAddress) GetAddress(context.Context, uuid.UUID, uuid.UUID) (domain.Address, error) {
	return s.stale, nil
}

func TestUpdateAddressLosesARace(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	me := seed(t, s)
	a, err := at(s, now).CreateAddress(ctx, me, uuid.NewV7(), home)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := s.GetAddress(ctx, me, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = at(s, now).UpdateAddress(ctx, me, a.ID, home); err != nil {
		t.Fatal(err)
	}
	_, err = at(staleAddress{Accounts: s, stale: stale}, now).UpdateAddress(ctx, me, a.ID, home)
	if !errors.Is(err, apperr.ErrTryAgain) {
		t.Fatalf("%v", err)
	}
}
