package domain_test

import (
	"testing"

	"github.com/shopnexus/server/internal/catalog/domain"
)

func TestMedianPrice(t *testing.T) {
	t.Parallel()
	s := func(prices ...int64) []domain.PriceSample {
		out := make([]domain.PriceSample, len(prices))
		for i, p := range prices {
			out[i] = domain.PriceSample{Price: p}
		}
		return out
	}
	for _, c := range []struct {
		in   []domain.PriceSample
		want int64
	}{
		{nil, 0},
		{s(5), 5},
		{s(9, 1, 5), 5},
		{s(4, 1, 3, 2), 2},
	} {
		if got := domain.MedianPrice(c.in); got != c.want {
			t.Errorf("%v: %d, want %d", c.in, got, c.want)
		}
	}
}
