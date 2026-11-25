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
	if _, specs, leafErr := s.Leaf(ctx, "phones.iphone"); leafErr != nil || len(specs) != 4 || specs[0].Key != "model" ||
		specs[0].Names[locale.Vietnamese] != "Dòng máy" || specs[0].Names[locale.English] != "Model" {
		t.Errorf("iphone specs %v, %v", specs, leafErr)
	}
	// spec_labels, which the release before reads, names the keys in
	// Vietnamese, and every key a category asks for is named in every
	// language.
	var stored map[string][]string
	if err = db.QueryRow(ctx, `SELECT jsonb_object_agg(code, spec_labels) FROM categories`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for _, c := range tree {
		names := make([]string, len(c.Specs))
		for i, sp := range c.Specs {
			names[i] = sp.Name
		}
		if c.Specs == nil || !slices.Equal(c.SpecLabels, names) || !slices.Equal(c.SpecLabels, stored[c.Code]) {
			t.Errorf("%s: specs %v, labels %v, stored %v", c.Code, c.Specs, c.SpecLabels, stored[c.Code])
		}
	}
	for _, tag := range locale.Supported[1:] {
		var missing []string
		if err = db.QueryRow(ctx, `SELECT coalesce(array_agg(DISTINCT k.key), '{}') FROM categories c
CROSS JOIN LATERAL unnest(c.spec_keys) AS k (key)
LEFT JOIN specs s ON s.key = k.key LEFT JOIN spec_names n ON n.key = k.key AND n.locale = @locale
WHERE s.key IS NULL OR n.key IS NULL`, pgx.StrictNamedArgs{"locale": string(tag)}).Scan(&missing); err != nil || len(missing) > 0 {
			t.Errorf("spec keys without a %s name: %v, %v", tag, missing, err)
		}
	}
	got, err := s.SpecNames(ctx, locale.English, []string{"color", "nope"})
	if err != nil || len(got) != 1 || got["color"] != "Color" {
		t.Errorf("spec names %v, %v", got, err)
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
		if c.Code != tree[i].Code || !slices.Equal(c.SpecLabels, tree[i].SpecLabels) || len(c.Specs) != len(tree[i].Specs) {
			t.Errorf("%d: %s, want %s", i, c.Code, tree[i].Code)
		}
		names[c.Code] = c.DisplayName
		for j, sp := range c.Specs {
			if sp.Key != tree[i].Specs[j].Key {
				t.Errorf("%s: spec %d %s, want %s", c.Code, j, sp.Key, tree[i].Specs[j].Key)
			}
			names[c.Code+"/"+sp.Key] = sp.Name
		}
	}
	for code, want := range map[string]string{"phones.iphone/color": "Color", "laptop.monitor/screen_size": "Screen size", "furniture.bed/dimensions": "Dimensions"} {
		if names[code] != want {
			t.Errorf("%s: %q, want %q", code, names[code], want)
		}
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
