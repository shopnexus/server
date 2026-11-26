package service_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/provider/chotot"
)

// FillAd writes a link read's draft and adds the condition and the specs
// the listing states, keyed for its category; a condition it does not
// state is used and guessed.
//
// UC-LST-04.
func TestFillAd(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ad := e.source.ad
	got, err := e.assist.FillAd(ctx, ad)
	if err != nil {
		t.Fatal(err)
	}
	d := got.Draft
	if d.Title != "iPhone 11 64GB Trắng, pin 81%" || d.CategoryCode != "phones.iphone" || d.Condition != catalogdomain.ConditionUsed ||
		d.Price == nil || *d.Price != 4_500_000 || strings.Contains(d.Description, "0909") || !got.ContactRemoved ||
		!slices.Equal(got.Guessed, []string{"condition"}) || got.NoItem != "" {
		t.Fatalf("%+v %+v", got, d)
	}
	want := []catalogdomain.Spec{{Key: "storage", Label: "Dung lượng", Value: "64 GB"}, {Key: "color", Label: "Màu", Value: "Trắng"}}
	if !slices.Equal(d.Specs, want) {
		t.Fatalf("specs %+v", d.Specs)
	}

	ad.Params = append(ad.Params, chotot.Param{Label: "Tình trạng", Value: "Mới"}, chotot.Param{Label: "Bảo hành", Value: "Hết"})
	if got, err = e.assist.FillAd(ctx, ad); err != nil || got.Draft.Condition != catalogdomain.ConditionNew || len(got.Guessed) != 0 ||
		len(got.Draft.Specs) != 2 {
		t.Fatalf("stated new: %+v %v", got, err)
	}

	e.model.mu.Lock()
	e.model.link = noItemReply
	e.model.mu.Unlock()
	if got, err = e.assist.FillAd(ctx, ad); err != nil || got.NoItem == "" || got.Draft.Title != "" {
		t.Fatalf("no item: %+v %v", got, err)
	}
}
