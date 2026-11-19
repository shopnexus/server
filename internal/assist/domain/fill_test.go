package domain_test

import (
	"reflect"
	"testing"

	"github.com/shopnexus/server/internal/assist/domain"
)

func samples(prices []int64, sold []bool) []domain.Sample {
	out := make([]domain.Sample, len(prices))
	for i := range prices {
		out[i] = domain.Sample{Price: prices[i], Sold: sold[i]}
	}
	return out
}

func TestMarketEstimate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []domain.Sample
		want *domain.Estimate
	}{
		{"under 5 is none", samples([]int64{100_000, 200_000, 300_000, 400_000}, []bool{true, true, true, true}), nil},
		{"none is none", nil, nil},
		// weights 1 1 1 1 1, W 5, C 1 2 3 4 5: 4C ≥ 5 at C 2, 4C ≥ 10 at C 3, 4C ≥ 15 at C 4
		{
			"active only, any order", samples([]int64{500_000, 100_000, 300_000, 200_000, 400_000}, []bool{false, false, false, false, false}),
			&domain.Estimate{Low: 200_000, Median: 300_000, High: 400_000, Source: domain.SourceMarket, SampleSize: 5},
		},
		// weights 1 2 1 2 1, W 7, C 1 3 4 6 7: 4C ≥ 7 at C 3, 4C ≥ 14 at C 4, 4C ≥ 21 at C 6
		{
			"sold weigh twice", samples([]int64{1_000_000, 1_200_000, 1_500_000, 1_800_000, 2_000_000}, []bool{false, true, false, true, false}),
			&domain.Estimate{Low: 1_200_000, Median: 1_500_000, High: 1_800_000, Source: domain.SourceMarket, SampleSize: 5},
		},
		// weights 1 1 1 2 2, W 7, C 1 2 3 5 7: 4C ≥ 7 at C 2, 4C ≥ 14 at C 5, 4C ≥ 21 at C 7
		{
			"sold pull the range up", samples([]int64{100_000, 200_000, 300_000, 400_000, 500_000}, []bool{false, false, false, true, true}),
			&domain.Estimate{Low: 200_000, Median: 400_000, High: 500_000, Source: domain.SourceMarket, SampleSize: 5},
		},
		{
			"rounded to 10,000", samples([]int64{1_234_567, 1_234_567, 1_235_000, 1_235_000, 1_244_999}, []bool{false, false, false, false, false}),
			&domain.Estimate{Low: 1_230_000, Median: 1_240_000, High: 1_240_000, Source: domain.SourceMarket, SampleSize: 5},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := domain.MarketEstimate(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
