package domain_test

import (
	"strings"
	"testing"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
)

var now = time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)

func TestNewCode(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for range 200 {
		c := domain.NewCode(now)
		if len(c.Code) != 9 || !strings.HasPrefix(c.Code, "SN-") || strings.ContainsAny(c.Code[3:], "01OIL") {
			t.Fatalf("code %q", c.Code)
		}
		if !c.ExpiresAt.Equal(now.Add(domain.CodeLife)) || !c.Live(now) || c.Live(c.ExpiresAt) {
			t.Fatalf("expiry %v", c.ExpiresAt)
		}
		seen[c.Code] = true
	}
	if len(seen) < 190 {
		t.Fatalf("only %d codes of 200 differ", len(seen))
	}
}

func TestHolds(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"Shop đồ cũ. SN-7K3Q9M":       true,
		"ma: sn-7k3q9m nhe":           true,
		"SN 7K3 Q9M":                  true,
		"SN–7K3Q9M":                   true,
		"SN_7K3Q9M":                   true,
		"SN-7K3Q9\nM":                 true,
		"SN-7K3Q9N":                   false,
		"":                            false,
		"SN-7K3Q9":                    false,
		"Giới thiệu về trang của tôi": false,
	} {
		if got := domain.Holds(text, "SN-7K3Q9M"); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
	if domain.Holds("anything", "") {
		t.Fatal("an empty code matched")
	}
}

func TestParseProfileURL(t *testing.T) {
	t.Parallel()
	const oid = "0123456789abcdef0123456789abcdef"
	for _, ok := range []string{
		"https://www.chotot.com/user/" + oid,
		"http://chotot.com/user/" + oid + "/",
		"www.chotot.com/user/" + strings.ToUpper(oid) + "?src=share",
		"  chotot.com/user/" + oid + "#top ",
	} {
		got, err := domain.ParseProfileURL(ok)
		if err != nil || got != oid {
			t.Errorf("%q: %q %v", ok, got, err)
		}
	}
	for _, bad := range []string{
		"", "https://www.chotot.com/" + oid, "https://www.chotot.com/user/" + oid[:31],
		"https://evil.com/user/" + oid, "https://chotot.com.evil.com/user/" + oid, "ftp://chotot.com/user/" + oid,
		"https://www.chotot.com/user/" + oid + "/ads", "https://x@www.chotot.com/user/" + oid,
		"https://www.chotot.com/user/" + strings.Repeat("g", 32), "https://www.chotot.com/" + strings.Repeat("a", 3000),
	} {
		if _, err := domain.ParseProfileURL(bad); err != domain.ErrInvalidProfileURL { //nolint:errorlint // the sentinel itself
			t.Errorf("%q: %v", bad, err)
		}
	}
	if domain.ProfileURL(oid) != "https://www.chotot.com/user/"+oid || domain.AdURL(123) != "https://www.chotot.com/123.htm" {
		t.Fatal("addresses")
	}
}

func TestNextSync(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		failures int
		jitter   float64
		want     time.Duration
	}{
		{0, 0, time.Hour},
		{0, 0.5, time.Hour + 5*time.Minute},
		{1, 0, 2 * time.Hour},
		{3, 0, 8 * time.Hour},
		{20, 0, 8 * time.Hour},
		{20, 0.99, 8*time.Hour + 9*time.Minute + 54*time.Second},
	} {
		if got := domain.NextSync(domain.SyncEvery, tc.failures, tc.jitter); got != tc.want {
			t.Errorf("%d failures, jitter %v: %v, want %v", tc.failures, tc.jitter, got, tc.want)
		}
	}
	if got := domain.NextSync(time.Second, 0, 0); got != time.Second {
		t.Errorf("a test cadence: %v", got)
	}
}

func TestNewLink(t *testing.T) {
	t.Parallel()
	l := domain.NewLink(uuid.NewV7(), uuid.NewV7(), "oid", 7, "Na", "", now)
	if !l.AutoImport || !l.FollowUpdates || !l.RemindHide || l.SeenThrough != nil || l.CheckedAt != nil {
		t.Fatalf("%+v", l)
	}
}

func TestImportStates(t *testing.T) {
	t.Parallel()
	listing := uuid.NewV7()
	imp := domain.NewImport(uuid.NewV7(), uuid.NewV7(), 9, "oid", now)
	if !imp.Settled(now.Add(domain.ImportStale-time.Second)) || imp.Settled(now.Add(domain.ImportStale)) {
		t.Fatal("a running import")
	}
	imp.Listed(listing, []string{domain.FieldCondition}, 500_000, now)
	if imp.State != domain.Listed || *imp.ListingID != listing || imp.Review[0] != "condition" || !imp.Settled(now.Add(1000*time.Hour)) {
		t.Fatalf("listed %+v", imp)
	}
	refused := catalogdomain.ErrInvalidListing.WithField("specs", apperr.MissingSpecs, "missing").
		WithField("category_code", apperr.Required, "required").WithMissingSpecs([]string{"storage"})
	imp.Drafted(domain.Draft{Title: "Máy"}, refused, 500_000, now)
	if imp.State != domain.Drafted || imp.ListingID != nil || strings.Join(imp.Review, ",") != "category_code,specs" ||
		imp.MissingSpecKeys[0] != "storage" || imp.Draft == nil {
		t.Fatalf("draft %+v", imp)
	}
	if !imp.Settled(now.Add(domain.DraftLife-time.Second)) || imp.Settled(now.Add(domain.DraftLife)) {
		t.Fatal("a draft's life")
	}
	imp.Ended(domain.Skipped, "listing_cap_reached", now)
	if imp.Settled(now) || imp.Draft != nil || len(imp.Review) != 0 {
		t.Fatalf("skipped %+v", imp)
	}
	imp.Ended(domain.Unsupported, domain.ReasonNoItem, now)
	if !imp.Settled(now.Add(1000 * time.Hour)) {
		t.Fatal("unsupported")
	}
}

func TestStatusOf(t *testing.T) {
	t.Parallel()
	listing, here := uuid.NewV7(), uuid.NewV7()
	at := func(state domain.ImportState, age time.Duration) *domain.Import {
		i := domain.NewImport(uuid.NewV7(), uuid.NewV7(), 9, "oid", now.Add(-age))
		i.State, i.Reason = state, "why"
		if state == domain.Listed {
			i.ListingID = &listing
		}
		return &i
	}
	for name, tc := range map[string]struct {
		realEstate bool
		imp        *domain.Import
		here       *uuid.UUID
		want       domain.AdStatus
	}{
		"new":                  {false, nil, nil, domain.AdStatus{State: domain.AdImportable}},
		"real estate":          {true, nil, &here, domain.AdStatus{State: domain.AdUnsupported, Reason: domain.ReasonRealEstate}},
		"here":                 {false, nil, &here, domain.AdStatus{State: domain.AdAlreadyHere, ListingID: &here}},
		"listed":               {false, at(domain.Listed, 0), &here, domain.AdStatus{State: domain.AdImported, ListingID: &listing}},
		"a draft":              {false, at(domain.Drafted, time.Hour), nil, domain.AdStatus{State: domain.AdImported}},
		"an expired draft":     {false, at(domain.Drafted, domain.DraftLife), nil, domain.AdStatus{State: domain.AdImportable}},
		"running":              {false, at(domain.Importing, time.Minute), nil, domain.AdStatus{State: domain.AdImporting}},
		"stale":                {false, at(domain.Importing, domain.ImportStale), nil, domain.AdStatus{State: domain.AdImportable}},
		"skipped":              {false, at(domain.Skipped, 0), nil, domain.AdStatus{State: domain.AdImportable, Reason: "why"}},
		"skipped, now here":    {false, at(domain.Skipped, 0), &here, domain.AdStatus{State: domain.AdAlreadyHere, ListingID: &here}},
		"unsupported":          {false, at(domain.Unsupported, 0), nil, domain.AdStatus{State: domain.AdUnsupported, Reason: "why"}},
		"listed, real estate?": {true, at(domain.Listed, 0), nil, domain.AdStatus{State: domain.AdImported, ListingID: &listing}},
	} {
		got := domain.StatusOf(tc.realEstate, tc.imp, tc.here, now)
		if got.State != tc.want.State || got.Reason != tc.want.Reason || (got.ListingID == nil) != (tc.want.ListingID == nil) ||
			(got.ListingID != nil && *got.ListingID != *tc.want.ListingID) {
			t.Errorf("%s: %+v, want %+v", name, got, tc.want)
		}
	}
}

func TestSameItem(t *testing.T) {
	t.Parallel()
	a, b := uuid.NewV7(), uuid.NewV7()
	mine := []domain.Mine{{ID: a, Title: "iPhone 13 Pro Max 256GB xanh", Price: 15_000_000}, {ID: b, Title: "Xe đạp", Price: 2_000_000}}
	for title, want := range map[string]*uuid.UUID{
		"IPHONE 13 PRO MAX 256GB XANH":                  &a,
		"iPhone 13 Pro Max 256GB":                       &a,
		"Bán gấp iPhone 13 Pro Max 256GB xanh, máy đẹp": &a,
		"iPhone 13":               &a,
		"iPhone 12 Pro Max 256GB": nil,
		"Xe đạp":                  nil, // another price
		"iPhone":                  nil, // one word is not enough
	} {
		got := domain.SameItem(title, 15_000_000, mine)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("%q: %v", title, got)
		}
	}
	if got := domain.SameItem("Xe dap", 2_000_000, mine); got == nil || *got != b {
		t.Errorf("without marks: %v", got)
	}
	if got := domain.SameItem("Xe đạp trẻ em", 2_000_000, mine); got == nil || *got != b {
		t.Errorf("xe đạp: %v", got)
	}
}

func TestDraftListing(t *testing.T) {
	t.Parallel()
	price := int64(500_000)
	d := domain.Draft{Title: "Máy", Price: &price, CategoryCode: "phones", Condition: catalogdomain.ConditionUsed, CommuneCode: "26734"}
	in := d.Listing()
	if in.Title != "Máy" || in.Price != price || !in.AllowOffers || in.CommuneCode != "26734" {
		t.Fatalf("%+v", in)
	}
	if (domain.Draft{}).Listing().Price != 0 {
		t.Fatal("no price")
	}
}
