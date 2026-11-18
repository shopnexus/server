package domain_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

var cats = []catalogdomain.Category{
	{Code: "phones", DisplayName: "Điện thoại"},
	{Code: "phones.iphone", ParentCode: "phones", DisplayName: "iPhone"},
	{Code: "phones.other", ParentCode: "phones", DisplayName: "Khác"},
	{Code: "misc", DisplayName: "Đồ linh tinh"}, // a root without .other
	{Code: "misc.toys", ParentCode: "misc", DisplayName: "Đồ chơi"},
}

func eightFrames() []domain.Photo {
	out := make([]domain.Photo, 8)
	for i := range out {
		out[i] = domain.Photo{UploadID: uuid.NewV7(), AtSeconds: new(float64(i))}
	}
	return out
}

func price(v int64) *int64 { return new(v) }

// good is an answer every rule keeps as it is.
func good() domain.Answer {
	return domain.Answer{
		IsItem: true, Title: "iPhone 11 64GB Trắng, pin 81%", CategoryCode: "phones.iphone", Condition: "used",
		Specs:       []catalogdomain.Spec{{Label: "Dung lượng", Value: "64 GB"}, {Label: "Pin", Value: "81%"}},
		Description: "Máy dùng 3 năm, chưa sửa chữa gì.", SpokenPrice: price(4_500_000), Negotiable: true,
		Kept: []int{2, 0, 3}, Estimate: &domain.Range{Low: 4_400_000, Median: 4_800_000, High: 5_100_000},
	}
}

func TestCleanKeepsAGoodAnswer(t *testing.T) {
	t.Parallel()
	frames := eightFrames()
	d, r, err := domain.Clean(good(), frames, 5, cats)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Draft{
		Title: "iPhone 11 64GB Trắng, pin 81%", CategoryCode: "phones.iphone", Condition: catalogdomain.ConditionUsed,
		Specs:       []catalogdomain.Spec{{Label: "Dung lượng", Value: "64 GB"}, {Label: "Pin", Value: "81%"}},
		Description: "Máy dùng 3 năm, chưa sửa chữa gì.", Price: price(4_500_000), AllowOffers: true,
		Photos: []uuid.UUID{frames[2].UploadID, frames[0].UploadID, frames[3].UploadID},
	}
	if !reflect.DeepEqual(d, want) {
		t.Fatalf("got %+v\nwant %+v", d, want)
	}
	if r == nil || *r != (domain.Range{Low: 4_400_000, Median: 4_800_000, High: 5_100_000}) {
		t.Fatalf("range %+v", r)
	}
}

func TestCleanRefusesNoItem(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*domain.Answer){
		"not an item":   func(a *domain.Answer) { a.IsItem = false },
		"blank title":   func(a *domain.Answer) { a.Title = " \n\t " },
		"no title text": func(a *domain.Answer) { a.Title = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a := good()
			change(&a)
			if _, _, err := domain.Clean(a, eightFrames(), 0, cats); !errors.Is(err, domain.ErrNoItem) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCleanRules(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("Đ", 80)
	cases := []struct {
		name   string
		change func(*domain.Answer)
		check  func(*testing.T, domain.Draft, *domain.Range, []domain.Photo)
	}{
		{"title spaces collapse", func(a *domain.Answer) { a.Title = "  iPhone   11\n64GB " }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Title != "iPhone 11 64GB" {
				t.Fatalf("%q", d.Title)
			}
		}},
		{"title cut at 70 characters", func(a *domain.Answer) { a.Title = long }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Title != strings.Repeat("Đ", 70) {
				t.Fatalf("%d runes", len([]rune(d.Title)))
			}
		}},
		{"a root becomes its other", func(a *domain.Answer) { a.CategoryCode = "phones" }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.CategoryCode != "phones.other" {
				t.Fatalf("%q", d.CategoryCode)
			}
		}},
		{"an unknown sub becomes its root's other", func(a *domain.Answer) { a.CategoryCode = "phones.nokia" }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.CategoryCode != "phones.other" {
				t.Fatalf("%q", d.CategoryCode)
			}
		}},
		{"an unknown root is empty", func(a *domain.Answer) { a.CategoryCode = "boats.sail" }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.CategoryCode != "" {
				t.Fatalf("%q", d.CategoryCode)
			}
		}},
		{"a root without other is empty", func(a *domain.Answer) { a.CategoryCode = "misc" }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.CategoryCode != "" {
				t.Fatalf("%q", d.CategoryCode)
			}
		}},
		{"spaces around a code", func(a *domain.Answer) { a.CategoryCode = " misc.toys " }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.CategoryCode != "misc.toys" {
				t.Fatalf("%q", d.CategoryCode)
			}
		}},
		{"an unknown condition is used", func(a *domain.Answer) { a.Condition = "broken" }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Condition != catalogdomain.ConditionUsed {
				t.Fatalf("%q", d.Condition)
			}
		}},
		{"like new stays", func(a *domain.Answer) { a.Condition = "like_new" }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Condition != catalogdomain.ConditionLikeNew {
				t.Fatalf("%q", d.Condition)
			}
		}},
		{"description trimmed and cut at 1500", func(a *domain.Answer) { a.Description = "  " + strings.Repeat("ă", 1600) + "  " }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Description != strings.Repeat("ă", 1500) {
				t.Fatalf("%d runes", len([]rune(d.Description)))
			}
		}},
		{"no speech, no description", func(a *domain.Answer) { a.Description = "   " }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Description != "" {
				t.Fatalf("%q", d.Description)
			}
		}},
		{"specs trimmed, empties dropped, long cut", func(a *domain.Answer) {
			a.Specs = []catalogdomain.Spec{
				{Label: " Màu ", Value: " Trắng "},
				{Label: "", Value: "x"},
				{Label: "Pin", Value: "  "},
				{Label: strings.Repeat("l", 60), Value: strings.Repeat("v", 60)},
			}
		}, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			want := []catalogdomain.Spec{{Label: "Màu", Value: "Trắng"}, {Label: strings.Repeat("l", 50), Value: strings.Repeat("v", 50)}}
			if !reflect.DeepEqual(d.Specs, want) {
				t.Fatalf("%+v", d.Specs)
			}
		}},
		{"at most 20 specs", func(a *domain.Answer) {
			a.Specs = nil
			for range 25 {
				a.Specs = append(a.Specs, catalogdomain.Spec{Label: "a", Value: "b"})
			}
		}, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if len(d.Specs) != 20 {
				t.Fatalf("%d specs", len(d.Specs))
			}
		}},
		{"no specs is an empty list", func(a *domain.Answer) { a.Specs = nil }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Specs == nil || len(d.Specs) != 0 {
				t.Fatalf("%#v", d.Specs)
			}
		}},
		{"a price under 10,000 is dropped", func(a *domain.Answer) { a.SpokenPrice = price(5_000) }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Price != nil {
				t.Fatalf("%d", *d.Price)
			}
		}},
		{"a price over 1,000,000,000 is dropped", func(a *domain.Answer) { a.SpokenPrice = price(2_000_000_000) }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Price != nil {
				t.Fatalf("%d", *d.Price)
			}
		}},
		{"the edges are prices", func(a *domain.Answer) { a.SpokenPrice = price(10_000) }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Price == nil || *d.Price != 10_000 {
				t.Fatalf("%v", d.Price)
			}
		}},
		{"no price said", func(a *domain.Answer) { a.SpokenPrice = nil }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.Price != nil {
				t.Fatalf("%d", *d.Price)
			}
		}},
		{"not negotiable", func(a *domain.Answer) { a.Negotiable = false }, func(t *testing.T, d domain.Draft, _ *domain.Range, _ []domain.Photo) {
			t.Helper()
			if d.AllowOffers {
				t.Fatal("offers allowed")
			}
		}},
		{"bad and repeated frames dropped, order kept", func(a *domain.Answer) { a.Kept = []int{4, -1, 9, 4, 1, 8} }, func(t *testing.T, d domain.Draft, _ *domain.Range, f []domain.Photo) {
			t.Helper()
			if !slices.Equal(d.Photos, []uuid.UUID{f[4].UploadID, f[1].UploadID}) {
				t.Fatalf("%v", d.Photos)
			}
		}},
		{"at most 6 frames", func(a *domain.Answer) { a.Kept = []int{0, 1, 2, 3, 4, 5, 6, 7} }, func(t *testing.T, d domain.Draft, _ *domain.Range, f []domain.Photo) {
			t.Helper()
			if len(d.Photos) != 6 || d.Photos[5] != f[5].UploadID {
				t.Fatalf("%v", d.Photos)
			}
		}},
		{"no valid frame takes the sharpest", func(a *domain.Answer) { a.Kept = []int{12, -3} }, func(t *testing.T, d domain.Draft, _ *domain.Range, f []domain.Photo) {
			t.Helper()
			if !slices.Equal(d.Photos, []uuid.UUID{f[5].UploadID}) {
				t.Fatalf("%v", d.Photos)
			}
		}},
		{"a range is rounded to 10,000", func(a *domain.Answer) {
			a.Estimate = &domain.Range{Low: 4_444_999, Median: 4_445_000, High: 5_104_000}
		}, func(t *testing.T, _ domain.Draft, r *domain.Range, _ []domain.Photo) {
			t.Helper()
			if r == nil || *r != (domain.Range{Low: 4_440_000, Median: 4_450_000, High: 5_100_000}) {
				t.Fatalf("%+v", r)
			}
		}},
		{"a range out of order is dropped", func(a *domain.Answer) {
			a.Estimate = &domain.Range{Low: 5_000_000, Median: 4_800_000, High: 5_100_000}
		}, func(t *testing.T, _ domain.Draft, r *domain.Range, _ []domain.Photo) {
			t.Helper()
			if r != nil {
				t.Fatalf("%+v", r)
			}
		}},
		{"a range under the floor is dropped", func(a *domain.Answer) {
			a.Estimate = &domain.Range{Low: 5_000, Median: 20_000, High: 30_000}
		}, func(t *testing.T, _ domain.Draft, r *domain.Range, _ []domain.Photo) {
			t.Helper()
			if r != nil {
				t.Fatalf("%+v", r)
			}
		}},
		{"a range over the ceiling is dropped", func(a *domain.Answer) {
			a.Estimate = &domain.Range{Low: 900_000_000, Median: 950_000_000, High: 1_200_000_000}
		}, func(t *testing.T, _ domain.Draft, r *domain.Range, _ []domain.Photo) {
			t.Helper()
			if r != nil {
				t.Fatalf("%+v", r)
			}
		}},
		{"no range", func(a *domain.Answer) { a.Estimate = nil }, func(t *testing.T, _ domain.Draft, r *domain.Range, _ []domain.Photo) {
			t.Helper()
			if r != nil {
				t.Fatalf("%+v", r)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a := good()
			c.change(&a)
			frames := eightFrames()
			d, r, err := domain.Clean(a, frames, 5, cats)
			if err != nil {
				t.Fatal(err)
			}
			c.check(t, d, r, frames)
		})
	}
}

// The schema requires exactly the answer's fields, as strict mode needs.
func TestAnswerSchemaMatchesAnswer(t *testing.T) {
	t.Parallel()
	var s struct {
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties bool                       `json:"additionalProperties"` //nolint:tagliatelle // JSON Schema's own name
	}
	if err := json.Unmarshal(domain.AnswerSchema, &s); err != nil {
		t.Fatal(err)
	}
	var tags []string
	for f := range reflect.TypeFor[domain.Answer]().Fields() {
		tags = append(tags, strings.Split(f.Tag.Get("json"), ",")[0])
	}
	slices.Sort(tags)
	req := slices.Clone(s.Required)
	slices.Sort(req)
	if !slices.Equal(tags, req) || len(s.Properties) != len(tags) || s.AdditionalProperties {
		t.Fatalf("schema %v, answer %v", req, tags)
	}
	var a domain.Answer
	if err := json.Unmarshal([]byte(`{"is_item":true,"title":"t","category_code":"c","condition":"used","specs":[],
"description":"","spoken_price":null,"negotiable":false,"kept":[0],"estimate":null}`), &a); err != nil || a.SpokenPrice != nil || a.Estimate != nil {
		t.Fatalf("%+v %v", a, err)
	}
}

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

func TestModelEstimate(t *testing.T) {
	t.Parallel()
	if domain.ModelEstimate(nil) != nil {
		t.Fatal("a range from nothing")
	}
	got := domain.ModelEstimate(&domain.Range{Low: 1_000_000, Median: 1_200_000, High: 1_500_000})
	want := &domain.Estimate{Low: 1_000_000, Median: 1_200_000, High: 1_500_000, Source: domain.SourceModel, SampleSize: 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}
