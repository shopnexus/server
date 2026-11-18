package domain_test

import (
	"errors"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
)

var draft = domain.AddressDraft{Name: " Hân ", Phone: "0912345678", Street: " 12 Lý Thường Kiệt ", CommuneCode: "00004", Kind: domain.AddressHome}

// addressField returns the problem err reports for name.
func addressField(err error, name string) string {
	if e, ok := errors.AsType[*apperr.Error](err); ok {
		return e.Fields[name]
	}
	return ""
}

func TestNewAddress(t *testing.T) {
	t.Parallel()
	owner := uuid.NewV7()
	a, err := domain.NewAddress(uuid.NewV7(), owner, draft, true, now)
	if err != nil || a.Name() != "Hân" || a.Street() != "12 Lý Thường Kiệt" || a.Phone() != "+84912345678" ||
		a.CommuneCode() != "00004" || a.Kind() != domain.AddressHome || a.AccountID() != owner || a.IsDefault() || !a.CreatedAt().Equal(now) || a.WriteID() != a.ID() {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestAddressRules(t *testing.T) {
	t.Parallel()
	for field, c := range map[string]struct {
		draft  domain.AddressDraft
		exists bool
	}{
		"name":         {domain.AddressDraft{Name: "  ", Phone: "0912345678", Street: "12 Lý", CommuneCode: "00004", Kind: "home"}, true},
		"phone":        {domain.AddressDraft{Name: "Hân", Phone: "12345", Street: "12 Lý", CommuneCode: "00004", Kind: "home"}, true},
		"street":       {domain.AddressDraft{Name: "Hân", Phone: "0912345678", Street: strings.Repeat("a", 201), CommuneCode: "00004", Kind: "home"}, true},
		"commune_code": {domain.AddressDraft{Name: "Hân", Phone: "0912345678", Street: "12 Lý", CommuneCode: "nope", Kind: "home"}, false},
		"kind":         {domain.AddressDraft{Name: "Hân", Phone: "0912345678", Street: "12 Lý", CommuneCode: "00004", Kind: "office"}, true},
	} {
		_, err := domain.NewAddress(uuid.NewV7(), uuid.NewV7(), c.draft, c.exists, now)
		if !errors.Is(err, domain.ErrInvalidAddress) || addressField(err, field) == "" {
			t.Errorf("%s: %v", field, err)
		}
	}
	long := draft
	long.Name = strings.Repeat("ữ", 51)
	if _, err := domain.NewAddress(uuid.NewV7(), uuid.NewV7(), long, true, now); addressField(err, "name") == "" {
		t.Errorf("51 characters: %v", err)
	}
	long.Name = strings.Repeat("ữ", 50)
	long.Street = strings.Repeat("ữ", 200)
	if _, err := domain.NewAddress(uuid.NewV7(), uuid.NewV7(), long, true, now); err != nil {
		t.Errorf("50 and 200 Vietnamese characters: %v", err)
	}
	_, err := domain.NewAddress(uuid.NewV7(), uuid.NewV7(), domain.AddressDraft{}, false, now)
	if e, ok := errors.AsType[*apperr.Error](err); !ok || len(e.Fields) != 5 {
		t.Errorf("every field at once: %v", err)
	}
}

func TestAddressKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.AddressKind{domain.AddressHome, domain.AddressWork, domain.AddressOther} {
		d := draft
		d.Kind = kind
		a, err := domain.NewAddress(uuid.NewV7(), uuid.NewV7(), d, true, now)
		if err != nil || a.Kind() != kind {
			t.Errorf("%s: %v %v", kind, a.Kind(), err)
		}
	}
	for _, kind := range []domain.AddressKind{"", "Home", "office", "home "} {
		d := draft
		d.Kind = kind
		_, err := domain.NewAddress(uuid.NewV7(), uuid.NewV7(), d, true, now)
		if e, ok := errors.AsType[*apperr.Error](err); !ok || !errors.Is(err, domain.ErrInvalidAddress) || len(e.Fields) != 1 || e.Fields["kind"] == "" {
			t.Errorf("kind %q: %v", kind, err)
		}
	}
}

func TestEditAddress(t *testing.T) {
	t.Parallel()
	a, err := domain.NewAddress(uuid.NewV7(), uuid.NewV7(), draft, true, now)
	if err != nil {
		t.Fatal(err)
	}
	a.StoredAsDefault()
	if err = a.Edit(domain.AddressDraft{Name: "Minh", Phone: "+84 98 765 4321", Street: "1 Lê Lợi", CommuneCode: "00007", Kind: domain.AddressWork}, true); err != nil {
		t.Fatal(err)
	}
	if a.Name() != "Minh" || a.Phone() != "+84987654321" || a.Street() != "1 Lê Lợi" || a.CommuneCode() != "00007" ||
		a.Kind() != domain.AddressWork || !a.IsDefault() {
		t.Fatalf("%+v", a)
	}
	if err = a.Edit(domain.AddressDraft{Name: "Lan", Phone: "0912345678", Street: "", CommuneCode: "00004", Kind: domain.AddressHome}, true); addressField(err, "street") == "" {
		t.Fatalf("an empty street: %v", err)
	}
	if err = a.Edit(domain.AddressDraft{Name: "Lan", Phone: "0912345678", Street: "2 Lý", CommuneCode: "00004", Kind: "office"}, true); addressField(err, "kind") == "" {
		t.Fatalf("an unknown kind: %v", err)
	}
	if a.Name() != "Minh" || a.Kind() != domain.AddressWork {
		t.Fatalf("a refused edit changed the address: %+v", a)
	}
}
