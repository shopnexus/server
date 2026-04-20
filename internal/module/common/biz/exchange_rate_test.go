package commonbiz_test

import (
	"testing"

	commonbiz "shopnexus-server/internal/module/common/biz"
)

func TestConvertAmount_SameCurrency(t *testing.T) {
	got := commonbiz.ConvertAmountPure(100_000, "VND", "VND", map[string]float64{"VND": 25000})
	if got != 100_000 {
		t.Errorf("same currency: got %d, want 100000", got)
	}
}

func TestConvertAmount_USDtoVND(t *testing.T) {
	// $10.00 = 1000 cents → 10 * 25000 = 250,000 VND (VND has 0 decimals)
	got := commonbiz.ConvertAmountPure(1000, "USD", "VND", map[string]float64{"VND": 25000})
	if got != 250_000 {
		t.Errorf("USD→VND: got %d, want 250000", got)
	}
}

func TestConvertAmount_VNDtoUSD(t *testing.T) {
	// 250,000 VND → 250000/25000 = 10 USD → 1000 cents
	got := commonbiz.ConvertAmountPure(250_000, "VND", "USD", map[string]float64{"VND": 25000})
	if got != 1000 {
		t.Errorf("VND→USD: got %d, want 1000", got)
	}
}

func TestConvertAmount_JPYtoVND(t *testing.T) {
	// 10,000 JPY (0 decimals) → USD: 10000/155 = 64.516... → VND: *25000 = 1,612,903
	got := commonbiz.ConvertAmountPure(10_000, "JPY", "VND",
		map[string]float64{"JPY": 155, "VND": 25000})
	want := int64(1_612_903)
	if got < want-1 || got > want+1 {
		t.Errorf("JPY→VND: got %d, want %d (±1)", got, want)
	}
}

func TestConvertAmount_MissingRate(t *testing.T) {
	// Unknown rate → return amount unchanged (fail-open display)
	got := commonbiz.ConvertAmountPure(100_000, "VND", "XYZ", map[string]float64{"VND": 25000})
	if got != 100_000 {
		t.Errorf("missing rate: got %d, want 100000 (passthrough)", got)
	}
}
