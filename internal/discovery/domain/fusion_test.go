package domain_test

import (
	"testing"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
)

func TestLikeTerms(t *testing.T) {
	t.Parallel()
	phrases, boosts := domain.LikeTerms("Xe đạp Asama", "vehicles.bicycle", 1_000_000)
	if len(phrases) != 1 || phrases[0].Text != "Xe đạp Asama" || phrases[0].Weight != 1 {
		t.Fatalf("%+v", phrases)
	}
	if len(boosts) != 2 || boosts[0].Kind != catalogdomain.BoostCategory || boosts[0].Code != "vehicles.bicycle" ||
		boosts[1].Kind != catalogdomain.BoostPrice || boosts[1].PriceMin != 700_000 || boosts[1].PriceMax != 1_300_000 {
		t.Fatalf("%+v", boosts)
	}
}

func TestWeights(t *testing.T) {
	t.Parallel()
	if domain.FuseQuery != (catalogdomain.Weights{BM25: 1, Text: 1, Photo: 0.5, Boost: 0.75}) ||
		domain.FuseLike != (catalogdomain.Weights{BM25: 0.5, Text: 1, Photo: 1, Boost: 0.5}) ||
		domain.FusePhoto != (catalogdomain.Weights{Photo: 1}) {
		t.Fatal("weights drifted from the spec")
	}
}
