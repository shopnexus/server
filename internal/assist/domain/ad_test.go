package domain_test

import (
	"slices"
	"testing"

	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

func TestAdCondition(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]catalogdomain.Condition{
		"Đã sử dụng (chưa sửa chữa)": catalogdomain.ConditionUsed,
		"Đã sử dụng (qua sửa chữa)":  catalogdomain.ConditionUsed,
		"Mới":                        catalogdomain.ConditionNew,
		"Như mới":                    catalogdomain.ConditionLikeNew,
		"Mới 99%":                    catalogdomain.ConditionLikeNew,
		"Đang cập nhật":              "",
	} {
		got, ok := domain.AdCondition([]catalogdomain.Spec{{Label: "Hãng", Value: "Apple"}, {Label: "Tình trạng", Value: value}})
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v, want %q", value, got, ok, want)
		}
	}
	if _, ok := domain.AdCondition([]catalogdomain.Spec{{Label: "Hãng", Value: "Mới"}}); ok {
		t.Error("no condition field")
	}
}

func TestAdSpecs(t *testing.T) {
	t.Parallel()
	got := domain.AdSpecs([]catalogdomain.Spec{
		{Label: "Hãng", Value: "Apple"}, {Label: "Màu sắc", Value: " Trắng "}, {Label: "Tình trạng", Value: "Mới"}, {Label: "Dung lượng", Value: "64 GB"},
	})
	want := []catalogdomain.Spec{{Label: "Hãng", Value: "Apple"}, {Label: "Màu", Value: "Trắng"}, {Label: "Dung lượng", Value: "64 GB"}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
}
