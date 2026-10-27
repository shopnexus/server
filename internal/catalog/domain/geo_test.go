package domain_test

import (
	"math"
	"testing"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

func TestBlur(t *testing.T) {
	t.Parallel()
	p := domain.Point{Lat: 10.776889, Lng: 106.700806}
	seen := map[domain.Point]bool{}
	for range 200 {
		b := domain.Blur(p)
		if d := domain.Distance(p, b); d > 1.3 {
			t.Fatalf("moved %.2f km", d)
		}
		for _, v := range []float64{b.Lat, b.Lng} {
			if r := math.Abs(v*100 - math.Round(v*100)); r > 1e-6 {
				t.Fatalf("%v is off the 0.01 grid", b)
			}
		}
		seen[b] = true
	}
	if len(seen) < 2 {
		t.Fatal("every blur landed on the same cell")
	}
}

func TestDistance(t *testing.T) {
	t.Parallel()
	// Hồ Gươm to Nhà hát Lớn Hà Nội, about 0.7 km.
	if d := domain.Distance(domain.Point{Lat: 21.0287, Lng: 105.8524}, domain.Point{Lat: 21.0245, Lng: 105.8577}); d < 0.6 || d > 0.8 {
		t.Fatalf("%.3f km", d)
	}
}
