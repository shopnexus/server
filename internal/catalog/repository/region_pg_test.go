package repository_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestRegions(t *testing.T) {
	t.Parallel()
	s := repository.NewRegions(pgtest.New(t))
	ctx := context.Background()
	provinces, err := s.Provinces(ctx)
	if err != nil || len(provinces) != 34 {
		t.Fatalf("%d provinces, %v", len(provinces), err)
	}
	communes, err := s.Communes(ctx, "01")
	if err != nil || len(communes) < 100 || communes[0].ProvinceCode != "01" || communes[0].Lat == 0 {
		t.Fatalf("%d communes of Hà Nội, %v", len(communes), err)
	}
	found, err := s.FindPlaces(ctx, []string{"Hà Nội", "hà nội", "Hà Nội", "Không có nơi này"})
	if hn := found["Hà Nội"]; err != nil || len(hn) != 1 || hn[0].Lat < 20 || hn[0].Lat > 22 {
		t.Errorf("Hà Nội: %v %v", found, err)
	}
	if len(found["hà nội"]) != 1 || len(found) != 2 {
		t.Errorf("by name as asked, case aside, an unknown place left out: %v", found)
	}
}

func TestPlaces(t *testing.T) {
	t.Parallel()
	s := repository.NewRegions(pgtest.New(t))
	got, err := s.Places(context.Background(), []string{"00004", "26704", "99999"})
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, p := range got {
		if p.DisplayName == "" || p.Province == "" || p.ProvinceCode == "" || p.Lat == 0 {
			t.Errorf("%+v", p)
		}
	}
}

func TestCategories(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewCategories(db)
	ctx := context.Background()
	tree, err := s.Tree(ctx, locale.Vietnamese)
	if err != nil || len(tree) != 97 || tree[0].Code != "phones" || tree[0].ParentCode != "" {
		t.Fatalf("%d categories, first %+v, %v", len(tree), tree[0], err)
	}
	for key, want := range map[string]bool{"phones.iphone": true, "phones": false, "other.other": true, "nope": false} {
		if got, _, _ := s.Leaf(ctx, key); got != want {
			t.Errorf("%s: %v", key, got)
		}
	}
	if _, labels, err := s.Leaf(ctx, "phones.iphone"); err != nil || len(labels) != 4 || labels[0] != "Dòng máy" {
		t.Errorf("iphone specs %v, %v", labels, err)
	}
	for _, c := range tree {
		if c.SpecLabels == nil {
			t.Errorf("%s: nil spec labels", c.Code)
		}
	}
	// Every category has a name in every language, so none reads Vietnamese
	// in English.
	for _, tag := range locale.Supported[1:] {
		var missing []string
		if err = db.QueryRow(ctx, `SELECT coalesce(array_agg(c.code), '{}') FROM categories c
LEFT JOIN category_names n ON n.code = c.code AND n.locale = @locale WHERE n.code IS NULL`,
			pgx.StrictNamedArgs{"locale": string(tag)}).Scan(&missing); err != nil || len(missing) > 0 {
			t.Errorf("%s lacks %v, %v", tag, missing, err)
		}
	}
	en, err := s.Tree(ctx, locale.English)
	if err != nil || len(en) != len(tree) {
		t.Fatalf("%d English categories, %v", len(en), err)
	}
	names := map[string]string{}
	for i, c := range en {
		if c.Code != tree[i].Code || !slices.Equal(c.SpecLabels, tree[i].SpecLabels) {
			t.Errorf("%d: %s, want %s", i, c.Code, tree[i].Code)
		}
		names[c.Code] = c.DisplayName
	}
	for code, want := range map[string]string{"phones": "Phones", "fashion": "Fashion", "phones.other": "Other", "other.other": "Other"} {
		if names[code] != want {
			t.Errorf("%s: %q, want %q", code, names[code], want)
		}
	}
}

func TestNearestCommune(t *testing.T) {
	t.Parallel()
	r := repository.NewRegions(pgtest.New(t))
	ctx := context.Background()
	places, err := r.Places(ctx, []string{"00004"})
	if err != nil || len(places) != 1 {
		t.Fatalf("%v %v", places, err)
	}
	p, err := r.Nearest(ctx, domain.Point{Lat: places[0].Lat + 0.001, Lng: places[0].Lng})
	if err != nil || p.Code != "00004" || p.Province == "" {
		t.Fatalf("%+v %v", p, err)
	}
}
