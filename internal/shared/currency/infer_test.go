package currency_test

import (
	"errors"
	"testing"

	sharedcurrency "shopnexus-server/internal/shared/currency"
)

func TestInfer(t *testing.T) {
	cases := []struct {
		country string
		want    string
	}{
		{"VN", "VND"},
		{"US", "USD"},
		{"DE", "EUR"},
		{"FR", "EUR"},
		{"JP", "JPY"},
		{"GB", "GBP"},
	}
	for _, c := range cases {
		got, err := sharedcurrency.Infer(c.country)
		if err != nil {
			t.Errorf("Infer(%q) err = %v", c.country, err)
			continue
		}
		if got != c.want {
			t.Errorf("Infer(%q) = %q, want %q", c.country, got, c.want)
		}
	}
}

func TestInfer_InvalidCountry(t *testing.T) {
	_, err := sharedcurrency.Infer("ZZ")
	if err == nil {
		t.Errorf("Infer(\"ZZ\") err = nil, want error")
	}
}

func TestInfer_MalformedInput(t *testing.T) {
	_, err := sharedcurrency.Infer("notacountry")
	if err == nil {
		t.Errorf("Infer(\"notacountry\") err = nil, want error")
	}
}

func TestInfer_NoCurrencyRegion(t *testing.T) {
	// Antarctica has no canonical currency.
	_, err := sharedcurrency.Infer("AQ")
	if err == nil {
		t.Errorf("Infer(\"AQ\") err = nil, want ErrNoCurrencyForRegion")
		return
	}
	if !errors.Is(err, sharedcurrency.ErrNoCurrencyForRegion) {
		t.Errorf("Infer(\"AQ\") err = %v, want ErrNoCurrencyForRegion", err)
	}
}
