package domain_test

import (
	"slices"
	"testing"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
)

func TestAccessoryDemotes(t *testing.T) {
	t.Parallel()
	both := []string{"phones.accessories", "camera.accessories"}
	for name, c := range map[string]struct {
		q    string
		f    catalogdomain.Filters
		want []string
	}{
		"a device":                      {"iphone 12", catalogdomain.Filters{}, both},
		"a phone accessory":             {"ốp lưng iphone 12", catalogdomain.Filters{}, []string{"camera.accessories"}},
		"folded, any case":              {"OP LUNG iPhone 12", catalogdomain.Filters{}, []string{"camera.accessories"}},
		"a cue beside the keywords":     {"cáp iphone", catalogdomain.Filters{}, []string{"camera.accessories"}},
		"a camera accessory":            {"Tripod Sony", catalogdomain.Filters{}, []string{"phones.accessories"}},
		"accessories of both":           {"phụ kiện", catalogdomain.Filters{}, nil},
		"a cue inside a word is no cue": {"tôp 10 iphone", catalogdomain.Filters{}, both},
		"filtered to a subcategory":     {"iphone 12", catalogdomain.Filters{CategoryCode: "phones.iphone"}, nil},
		"filtered to the accessories":   {"iphone 12", catalogdomain.Filters{CategoryCode: "phones.accessories"}, nil},
		"filtered to a root":            {"iphone 12", catalogdomain.Filters{CategoryRoot: "phones"}, []string{"phones.accessories"}},
		"filtered to a root with none":  {"xe đạp", catalogdomain.Filters{CategoryRoot: "vehicles"}, nil},
		"punctuation splits the words":  {"iphone 12,ốp lưng", catalogdomain.Filters{}, []string{"camera.accessories"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := domain.AccessoryDemotes(c.q, c.f)
			var codes []string
			for _, b := range got {
				if b.Kind != catalogdomain.BoostCategory || b.Weight != domain.AccessoryWeight || b.Weight >= 0 {
					t.Fatalf("%+v", b)
				}
				codes = append(codes, b.Code)
			}
			if !slices.Equal(codes, c.want) {
				t.Fatalf("got %v, want %v", codes, c.want)
			}
		})
	}
}
