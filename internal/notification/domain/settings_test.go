package domain_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/notification/domain"
)

// typeConstants reads every constant of type Type declared in the package's
// source, so a type added later is found without a list to keep.
func typeConstants(t *testing.T) []domain.Type {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Type
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			g, ok := decl.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				v, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if id, ok := v.Type.(*ast.Ident); !ok || id.Name != "Type" {
					continue
				}
				for _, val := range v.Values {
					lit, ok := val.(*ast.BasicLit)
					if !ok {
						t.Fatalf("%s: a Type constant that is not a literal", name)
					}
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					out = append(out, domain.Type(s))
				}
			}
		}
	}
	return out
}

func TestEveryTypeIsDescribed(t *testing.T) {
	t.Parallel()
	types := typeConstants(t)
	if len(types) < 19 {
		t.Fatalf("found %d types: %v", len(types), types)
	}
	for _, ty := range types {
		if !domain.Described(ty) {
			t.Errorf("type %s has no entry in types: no text, title or switch", ty)
		}
	}
}

func TestSwitchOf(t *testing.T) {
	t.Parallel()
	for ty, want := range map[domain.Type]domain.Switch{
		domain.TypeCounter:         domain.SwitchOffers,
		domain.TypeOfferVoided:     domain.SwitchOffers,
		domain.TypeCodRequested:    domain.SwitchOrders,
		domain.TypeOrderCancelled:  domain.SwitchOrders,
		domain.TypePriceDropped:    domain.SwitchPriceCuts,
		domain.TypeFollowedListing: domain.SwitchFollowed,
		domain.TypeReview:          domain.SwitchReviews,
		domain.TypeWatchMatched:    domain.SwitchNone,
		domain.TypeListingRejected: domain.SwitchNone,
		domain.TypeSuspended:       domain.SwitchNone,
	} {
		if got := domain.SwitchOf(ty); got != want {
			t.Errorf("%s: %q, want %q", ty, got, want)
		}
	}
}

// vn is a time on Vietnam's clock, UTC+7.
func vn(hour, minute int) time.Time {
	return time.Date(2026, 11, 14, hour, minute, 0, 0, time.FixedZone("ICT", 7*60*60))
}

func TestQuietHolds(t *testing.T) {
	t.Parallel()
	night := domain.Quiet{On: true, From: "22:00", To: "07:00"}
	lunch := domain.Quiet{On: true, From: "12:00", To: "13:30"}
	for name, c := range map[string]struct {
		q    domain.Quiet
		at   time.Time
		want bool
	}{
		"night, before":           {night, vn(21, 59), false},
		"night, from is inside":   {night, vn(22, 0), true},
		"night, past midnight":    {night, vn(0, 30), true},
		"night, a minute before":  {night, vn(6, 59), true},
		"night, to is outside":    {night, vn(7, 0), false},
		"night, midday":           {night, vn(12, 0), false},
		"lunch, from is inside":   {lunch, vn(12, 0), true},
		"lunch, inside":           {lunch, vn(13, 29), true},
		"lunch, to is outside":    {lunch, vn(13, 30), false},
		"lunch, morning":          {lunch, vn(8, 0), false},
		"off holds nothing":       {domain.Quiet{From: "22:00", To: "07:00"}, vn(23, 0), false},
		"read on Vietnam's clock": {night, time.Date(2026, 11, 14, 16, 0, 0, 0, time.UTC), true},
		"UTC morning is VN day":   {night, time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC), false},
	} {
		if got := c.q.Holds(c.at); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func TestPushes(t *testing.T) {
	t.Parallel()
	s := domain.DefaultSettings()
	s.Offers = false
	day, night := vn(10, 0), vn(23, 0)
	if s.Pushes(domain.SwitchOffers, day) {
		t.Error("offers off pushed")
	}
	if !s.Pushes(domain.SwitchOrders, day) || !s.Pushes(domain.SwitchNone, day) {
		t.Error("a switch on, or none, held by day")
	}
	if s.Pushes(domain.SwitchNone, night) || s.Pushes(domain.SwitchMessages, night) {
		t.Error("quiet hours let a push through")
	}
	s.Quiet.On = false
	if !s.Pushes(domain.SwitchMessages, night) {
		t.Error("quiet hours off held a push")
	}
}

func TestSettingsCheck(t *testing.T) {
	t.Parallel()
	if err := domain.DefaultSettings().Check(); err != nil {
		t.Fatalf("the defaults: %v", err)
	}
	for _, q := range []domain.Quiet{
		{On: true, From: "22:00", To: "22:00"},
		{From: "08:15", To: "08:15"},
		{On: true, From: "24:00", To: "07:00"},
		{On: true, From: "7:00", To: "22:00"},
		{On: true, From: "22:00", To: "07:60"},
		{On: true, From: "", To: "07:00"},
	} {
		s := domain.DefaultSettings()
		s.Quiet = q
		if err := s.Check(); !errors.Is(err, domain.ErrInvalidQuietHours) {
			t.Errorf("%+v: %v", q, err)
		}
	}
}
