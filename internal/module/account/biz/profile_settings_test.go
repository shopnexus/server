package accountbiz_test

import (
	"encoding/json"
	"testing"

	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
)

// Verify unknown JSONB keys are preserved after patch.
func TestMergeSettings_PreservesUnknownKeys(t *testing.T) {
	existing := json.RawMessage(`{"preferred_currency":"VND","theme":"dark"}`)
	merged, err := accountbiz.MergeSettings(existing, accountmodel.ProfileSettings{
		PreferredCurrency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(merged, &got)
	if got["preferred_currency"] != "USD" {
		t.Errorf("preferred_currency = %v, want USD", got["preferred_currency"])
	}
	if got["theme"] != "dark" {
		t.Errorf("theme should be preserved, got %v", got["theme"])
	}
}

// Empty existing -> typed fields only.
func TestMergeSettings_EmptyExisting(t *testing.T) {
	merged, err := accountbiz.MergeSettings(nil, accountmodel.ProfileSettings{
		PreferredCurrency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(merged, &got)
	if got["preferred_currency"] != "USD" {
		t.Errorf("preferred_currency = %v, want USD", got["preferred_currency"])
	}
}
