package domain

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

const (
	// MinMarketSample is how many similar listings a market range needs.
	MinMarketSample = 5
	// SoldWeight is what a sold listing counts in a market range; a listing
	// on sale counts 1.
	SoldWeight = 2
)

// Range is a price range in whole đồng.
type Range struct {
	Low    int64 `json:"low"`
	Median int64 `json:"median"`
	High   int64 `json:"high"`
}

// Answer is what the model returns for a video, in AnswerSchema's shape.
type Answer struct {
	IsItem      bool                 `json:"is_item"`
	Title       string               `json:"title"`
	Condition   string               `json:"condition"`
	Specs       []catalogdomain.Spec `json:"specs"`
	Description string               `json:"description"`
	SpokenPrice *int64               `json:"spoken_price"`
	Negotiable  bool                 `json:"negotiable"`
	Kept        []int                `json:"kept"`
	Estimate    *Range               `json:"estimate"`
}

// AnswerSchemaName names AnswerSchema in the request.
const AnswerSchemaName = "video_read"

// AnswerSchema is Answer as a strict JSON schema: every property required,
// none other allowed, null where the answer may have none.
var AnswerSchema = json.RawMessage(`{"type":"object","additionalProperties":false,
"required":["is_item","title","condition","specs","description","spoken_price","negotiable","kept","estimate"],
"properties":{"is_item":{"type":"boolean"},"title":{"type":"string"},
"condition":{"type":"string","enum":["new","like_new","used"]},
"specs":{"type":"array","maxItems":20,"items":{"type":"object","additionalProperties":false,"required":["label","value"],
"properties":{"label":{"type":"string"},"value":{"type":"string"}}}},
"description":{"type":"string"},"spoken_price":{"type":["integer","null"]},"negotiable":{"type":"boolean"},
"kept":{"type":"array","minItems":1,"maxItems":6,"items":{"type":"integer"}},
"estimate":{"anyOf":[{"type":"null"},{"type":"object","additionalProperties":false,"required":["low","median","high"],
"properties":{"low":{"type":"integer"},"median":{"type":"integer"},"high":{"type":"integer"}}}]}}}`)

// Clean turns the model's answer into a draft and its price range,
// keeping only values that pass the listing's rules. frames are the read's
// photos in the order the model saw them, and sharpest the index of the
// sharpest, the cover when the model kept none. An answer that shows no
// item, or has no title, is ErrNoItem. The draft has no category yet.
func Clean(a Answer, frames []Photo, sharpest int) (Draft, *Range, error) {
	d, err := cleanDraft(a.IsItem, a.Title, a.Condition, a.Specs, a.Description)
	if err != nil {
		return Draft{}, nil, err
	}
	d.AllowOffers = a.Negotiable

	if p := a.SpokenPrice; p != nil && *p >= catalogdomain.MinPrice && *p <= catalogdomain.MaxPrice {
		price := *p
		d.Price = &price
	}

	seen := make(map[int]bool, len(a.Kept))
	for _, i := range a.Kept {
		if i < 0 || i >= len(frames) || seen[i] || len(d.Photos) == MaxKept {
			continue
		}
		seen[i] = true
		d.Photos = append(d.Photos, frames[i].UploadID)
	}
	if len(d.Photos) == 0 && sharpest >= 0 && sharpest < len(frames) {
		d.Photos = append(d.Photos, frames[sharpest].UploadID)
	}
	return d, cleanRange(a.Estimate), nil
}

// cleanDraft keeps the fields both reads fill only where they pass the
// listing's rules: the title squeezed and cut, a known condition (else
// used), specs cut to size, the description cut, and an empty list of
// photos. No item, or no title, is ErrNoItem.
func cleanDraft(isItem bool, rawTitle, rawCondition string, rawSpecs []catalogdomain.Spec, rawDescription string) (Draft, error) {
	title := cut(strings.Join(strings.Fields(rawTitle), " "), catalogdomain.MaxTitle)
	if !isItem || title == "" {
		return Draft{}, ErrNoItem
	}
	d := Draft{
		Title: title, Condition: catalogdomain.Condition(rawCondition),
		Specs:       []catalogdomain.Spec{},
		Description: cut(strings.TrimSpace(rawDescription), catalogdomain.MaxDescription),
		Photos:      []uuid.UUID{},
	}

	switch d.Condition {
	case catalogdomain.ConditionNew, catalogdomain.ConditionLikeNew, catalogdomain.ConditionUsed:
	default:
		d.Condition = catalogdomain.ConditionUsed
	}

	for _, s := range rawSpecs {
		label := cut(strings.TrimSpace(s.Label), catalogdomain.MaxSpecText)
		value := cut(strings.TrimSpace(s.Value), catalogdomain.MaxSpecText)
		if label == "" || value == "" {
			continue
		}
		if len(d.Specs) == catalogdomain.MaxSpecs {
			break
		}
		d.Specs = append(d.Specs, catalogdomain.Spec{Label: label, Value: value})
	}
	return d, nil
}

// cleanRange keeps the model's range when it is ordered and within the
// listing's prices, rounded to 10,000; else nil.
func cleanRange(e *Range) *Range {
	if e == nil || catalogdomain.MinPrice > e.Low || e.Low > e.Median || e.Median > e.High || e.High > catalogdomain.MaxPrice {
		return nil
	}
	return &Range{Low: roundPrice(e.Low), Median: roundPrice(e.Median), High: roundPrice(e.High)}
}

// Sample is the price of a similar listing, whether it sold, and the whole
// days its sale took (0 on sale).
type Sample struct {
	Price      int64
	Sold       bool
	DaysToSell int
}

// MarketEstimate is the weighted quartiles of the samples' prices, nil
// under MinMarketSample samples. Sorted by price, a sample weighs
// SoldWeight when sold and 1 otherwise; the q-quantile is the price of the
// first sample whose running weight reaches q of the total. Each is
// rounded to 10,000.
func MarketEstimate(samples []Sample) *Estimate {
	if len(samples) < MinMarketSample {
		return nil
	}
	s := slices.Clone(samples)
	slices.SortStableFunc(s, func(a, b Sample) int { return cmp.Compare(a.Price, b.Price) })
	var total int64
	for _, x := range s {
		total += sampleWeight(x)
	}
	// quartile k is the price where 4 × the running weight first reaches k
	// × the total.
	quartile := func(k int64) int64 {
		var run int64
		for _, x := range s {
			run += sampleWeight(x)
			if 4*run >= k*total {
				return roundPrice(x.Price)
			}
		}
		return roundPrice(s[len(s)-1].Price)
	}
	return &Estimate{Low: quartile(1), Median: quartile(2), High: quartile(3), Source: SourceMarket, SampleSize: len(s)}
}

// ModelEstimate is the model's own range, nil when it gave none.
func ModelEstimate(r *Range) *Estimate {
	if r == nil {
		return nil
	}
	return &Estimate{Low: r.Low, Median: r.Median, High: r.High, Source: SourceModel, SampleSize: 0}
}

func sampleWeight(s Sample) int64 {
	if s.Sold {
		return SoldWeight
	}
	return 1
}

// roundPrice rounds to the nearest 10,000 đồng, half up.
func roundPrice(v int64) int64 { return (v + 5_000) / 10_000 * 10_000 }

// cut keeps the first n characters of s, trimmed again after the cut.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}
