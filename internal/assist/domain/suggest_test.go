package domain_test

import (
	"slices"
	"testing"

	"github.com/shopnexus/server2/internal/assist/domain"
)

func soldAfter(price int64, days int) domain.Sample {
	return domain.Sample{Price: price, Sold: true, DaysToSell: days}
}

func onSale(price int64) domain.Sample { return domain.Sample{Price: price} }

func TestSuggest(t *testing.T) {
	t.Parallel()
	est := func(median int64) domain.Estimate {
		return domain.Estimate{Median: median, Source: domain.SourceMarket}
	}
	cases := []struct {
		name    string
		samples []domain.Sample
		est     domain.Estimate
		want    []domain.Suggestion
	}{
		{
			"under 3 sold is none",
			[]domain.Sample{soldAfter(1_000_000, 2), soldAfter(1_100_000, 6), onSale(900_000), onSale(950_000)},
			est(1_000_000),
			[]domain.Suggestion{},
		},
		// days 2 6 10, median 6; the fast half is 1.0 (2) and 1.1 (6): price 1.05, days 4.
		{
			"the quicker half",
			[]domain.Sample{
				soldAfter(1_000_000, 2), soldAfter(1_100_000, 6), soldAfter(1_200_000, 10),
				onSale(1_300_000), onSale(1_400_000), onSale(1_500_000),
			},
			est(1_200_000),
			[]domain.Suggestion{
				{Kind: domain.SuggestFast, Price: 1_050_000, Days: 4},
				{Kind: domain.SuggestMarket, Price: 1_200_000, Days: 6},
			},
		},
		// days 1 5 9, median 5; the fast half is 1.5 (1) and 1.0 (5): 1.25 is not below 1.2.
		{
			"a fast half priced above the market is left out",
			[]domain.Sample{soldAfter(1_500_000, 1), soldAfter(1_000_000, 5), soldAfter(1_100_000, 9)},
			est(1_200_000),
			[]domain.Suggestion{{Kind: domain.SuggestMarket, Price: 1_200_000, Days: 5}},
		},
		{
			"every sale took as long",
			[]domain.Sample{soldAfter(1_000_000, 3), soldAfter(1_200_000, 3), soldAfter(1_400_000, 3)},
			est(1_300_000),
			[]domain.Suggestion{
				{Kind: domain.SuggestFast, Price: 1_200_000, Days: 3},
				{Kind: domain.SuggestMarket, Price: 1_300_000, Days: 3},
			},
		},
		// days 1 2 9, median 2; the fast half 1,234,567 and 1,250,000 has the
		// mean 1,242,283, rounded to 1,240,000, and days (1+2)/2 = 1.
		{
			"rounded to 10,000",
			[]domain.Sample{soldAfter(1_234_567, 1), soldAfter(1_250_000, 2), soldAfter(2_000_000, 9)},
			est(1_500_000),
			[]domain.Suggestion{
				{Kind: domain.SuggestFast, Price: 1_240_000, Days: 1},
				{Kind: domain.SuggestMarket, Price: 1_500_000, Days: 2},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.Suggest(c.samples, c.est); !slices.Equal(got, c.want) {
				t.Fatalf("%+v, want %+v", got, c.want)
			}
		})
	}
}
