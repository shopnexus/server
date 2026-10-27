package repository_test

import (
	"context"
	"testing"

	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
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
	if ok, _ := s.CommuneExists(ctx, "00004"); !ok {
		t.Error("Ba Đình is missing")
	}
	if ok, _ := s.CommuneExists(ctx, "99999"); ok {
		t.Error("an unknown commune exists")
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
