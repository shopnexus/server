package domain_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

func photoAnswer() domain.PhotoAnswer {
	return domain.PhotoAnswer{
		IsItem: true, Title: "iPhone 11 Trắng", CategoryCode: "phones.iphone", Condition: "used",
		Specs:       []catalogdomain.Spec{{Label: "Màu", Value: "Trắng"}},
		Description: "iPhone 11 màu trắng, mặt lưng kính, cụm 2 camera sau.",
		Estimate:    &domain.Range{Low: 4_000_000, Median: 4_500_000, High: 5_000_000},
	}
}

var photoCats = []catalogdomain.Category{
	{Code: "phones", DisplayName: "Điện thoại"},
	{Code: "phones.iphone", ParentCode: "phones", DisplayName: "iPhone"},
	{Code: "phones.other", ParentCode: "phones", DisplayName: "Khác"},
}

func TestCleanPhoto(t *testing.T) {
	t.Parallel()
	photo := uuid.NewV7()
	d, rng, err := domain.CleanPhoto(photoAnswer(), photo, photoCats)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "iPhone 11 Trắng" || d.CategoryCode != "phones.iphone" || d.Condition != catalogdomain.ConditionUsed ||
		d.Price != nil || d.AllowOffers || len(d.Photos) != 1 || d.Photos[0] != photo {
		t.Fatalf("%+v", d)
	}
	if rng == nil || rng.Median != 4_500_000 {
		t.Fatalf("range %+v", rng)
	}

	long := photoAnswer()
	long.Description = strings.Repeat("Máy màu trắng. ", 40)
	if d, _, err = domain.CleanPhoto(long, photo, photoCats); err != nil || utf8.RuneCountInString(d.Description) > domain.MaxPhotoDescription {
		t.Fatalf("%d characters: %v", utf8.RuneCountInString(d.Description), err)
	}

	for _, a := range []domain.PhotoAnswer{{IsItem: false, Title: "x"}, {IsItem: true, Title: "   "}} {
		if _, _, err = domain.CleanPhoto(a, photo, photoCats); !errors.Is(err, domain.ErrNoItem) {
			t.Fatalf("%+v: %v", a, err)
		}
	}
}
