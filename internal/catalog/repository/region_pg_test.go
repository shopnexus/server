package repository_test

import (
	"context"
	"testing"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
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
	s := repository.NewCategories(pgtest.New(t))
	ctx := context.Background()
	tree, err := s.Tree(ctx)
	if err != nil || len(tree) != 55 || tree[0].Code != "phones" || tree[0].ParentCode != "" {
		t.Fatalf("%d categories, first %+v, %v", len(tree), tree[0], err)
	}
	for key, want := range map[string]bool{"phones.iphone": true, "phones": false, "other.other": true, "nope": false} {
		if got, _ := s.IsLeaf(ctx, key); got != want {
			t.Errorf("%s: %v", key, got)
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
