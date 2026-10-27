package domain

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

// PromptVersion names the prompt and schema below; a change to either bumps
// it, so cached answers of the old prompt are no longer read.
const PromptVersion = 1

const (
	// ModelTimeout is how long a search waits for the model before it
	// searches the raw words alone.
	ModelTimeout = 4 * time.Second
	// CacheTTL is how long an answer is reused for the same query.
	CacheTTL = 7 * 24 * time.Hour
)

// Attr is what a signal of the model is about.
type Attr string

const (
	AttrPhrase    Attr = "phrase"
	AttrCategory  Attr = "category"
	AttrPrice     Attr = "price"
	AttrCondition Attr = "condition"
	AttrPlace     Attr = "place"
	AttrNearMe    Attr = "near_me"
	AttrFresh     Attr = "fresh"
	AttrOffers    Attr = "offers"
)

// Signal is one thing the model said matters, its values in priority order.
type Signal struct {
	Attr   Attr     `json:"attr"`
	Values []string `json:"values"`
}

// Understanding is the model's whole answer.
type Understanding struct {
	Understood string   `json:"understood"`
	Boosts     []Signal `json:"boosts"`
	Demotes    []Signal `json:"demotes"`
}

// Vocabulary resolves what the model named against real rows.
type Vocabulary struct {
	Categories map[string]string // code → display name, roots and subcategories
	Place      func(name string) (catalogdomain.Point, bool)
}

// Chip is a signal shown to the person, who may turn it off by its ID.
type Chip struct {
	ID    string `json:"id"`
	Kind  Attr   `json:"kind"`
	Label string `json:"label"`
}

// Compiled is an understanding resolved into catalog's search terms.
type Compiled struct {
	Phrases []catalogdomain.Phrase
	Boosts  []catalogdomain.Boost
	Chips   []Chip
	// Asked is every value the model named and Ran what became a term, for
	// the log line that says why a page looks as it does.
	Asked, Ran []string
}

// The weights are starting values in BM25 points, where a short title that
// matches the query scores about 1 to 7. The model never writes a number:
// it says which attributes matter and in what order, and these decide how
// much that moves a page. A set of sample queries is what would measure
// them.
var weight = map[Attr]float64{
	AttrPhrase: 1.0, AttrCategory: 1.5, AttrPrice: 1.5, AttrCondition: 0.75, AttrOffers: 0.75, AttrFresh: 1.0,
}

// RawWeight is what the person's own words are worth as a phrase.
const RawWeight = 0.5

// position is how much a signal's 1st, 2nd and 3rd value count; its length
// caps the values a signal may carry.
var position = []float64{1, 0.5, 0.33}

// rings are the distance boosts around a point.
var rings = []struct{ km, weight float64 }{{2, 1.25}, {5, 0.75}, {15, 0.4}}

// freshness are the age boosts, each a share of the fresh weight.
var freshness = []struct {
	age   time.Duration
	share float64
}{{3 * 24 * time.Hour, 1}, {7 * 24 * time.Hour, 0.5}}

var conditionLabel = map[catalogdomain.Condition]string{
	catalogdomain.ConditionNew: "Mới", catalogdomain.ConditionLikeNew: "Như mới", catalogdomain.ConditionUsed: "Đã dùng",
}

// Compile resolves u into phrases and boosts. A value that does not resolve
// is dropped: a page missing one signal is still worth serving. raw, the
// person's own words, is always a phrase unless the model named it. A chip
// in off is left out with its terms. origin is where the person searches
// from, nil when they gave no position.
func Compile(u Understanding, raw string, v Vocabulary, origin *catalogdomain.Point, now time.Time, off []string) Compiled {
	var c Compiled
	for _, group := range []struct {
		signals []Signal
		sign    float64
	}{{u.Boosts, 1}, {u.Demotes, -1}} {
		for _, s := range group.signals {
			for i, value := range s.Values {
				if i >= len(position) {
					break
				}
				mark := "+"
				if group.sign < 0 {
					mark = "-"
				}
				c.Asked = append(c.Asked, mark+string(s.Attr)+"="+value)
				id := fmt.Sprintf("%s-%d", s.Attr, i)
				if group.sign > 0 && slices.Contains(off, id) {
					continue
				}
				w := group.sign * position[i]
				label, ok := c.add(s.Attr, strings.TrimSpace(value), w, v, origin, now)
				if !ok {
					continue
				}
				c.Ran = append(c.Ran, fmt.Sprintf("%s%s=%s w=%+.2f", mark, s.Attr, value, w))
				if group.sign > 0 {
					c.Chips = append(c.Chips, Chip{ID: id, Kind: s.Attr, Label: label})
				}
			}
			if len(s.Values) == 0 && flag(s.Attr) {
				id := fmt.Sprintf("%s-0", s.Attr)
				if group.sign > 0 && slices.Contains(off, id) {
					continue
				}
				if label, ok := c.add(s.Attr, "", group.sign, v, origin, now); ok {
					c.Ran = append(c.Ran, fmt.Sprintf("%s w=%+.0f", s.Attr, group.sign))
					if group.sign > 0 {
						c.Chips = append(c.Chips, Chip{ID: id, Kind: s.Attr, Label: label})
					}
				}
			}
		}
	}
	key := QueryKey(raw)
	if key != "" && !slices.ContainsFunc(c.Phrases, func(p catalogdomain.Phrase) bool {
		return p.Weight > 0 && QueryKey(p.Text) == key
	}) {
		c.Phrases = append(c.Phrases, catalogdomain.Phrase{Text: strings.TrimSpace(raw), Weight: RawWeight})
	}
	return c
}

// flag reports an attribute that takes no value.
func flag(a Attr) bool { return a == AttrNearMe || a == AttrFresh || a == AttrOffers }

// add turns one value into terms at scale w (the sign times the position)
// and returns its chip's label, or false when it does not resolve.
func (c *Compiled) add(a Attr, value string, w float64, v Vocabulary, origin *catalogdomain.Point, now time.Time) (string, bool) {
	switch a {
	case AttrPhrase:
		if value == "" {
			return "", false
		}
		c.Phrases = append(c.Phrases, catalogdomain.Phrase{Text: value, Weight: w * weight[AttrPhrase]})
		return value, true
	case AttrCategory:
		name, ok := v.Categories[value]
		if !ok {
			return "", false
		}
		kind := catalogdomain.BoostCategoryRoot
		if strings.Contains(value, ".") {
			kind = catalogdomain.BoostCategory
		}
		c.Boosts = append(c.Boosts, catalogdomain.Boost{Kind: kind, Code: value, Weight: w * weight[AttrCategory]})
		return name, true
	case AttrPrice:
		lo, hi, ok := priceRange(value)
		if !ok {
			return "", false
		}
		c.Boosts = append(c.Boosts, catalogdomain.Boost{
			Kind: catalogdomain.BoostPrice, PriceMin: lo, PriceMax: hi, Weight: w * weight[AttrPrice],
		})
		return priceLabel(lo, hi), true
	case AttrCondition:
		cond := catalogdomain.Condition(value)
		label, ok := conditionLabel[cond]
		if !ok {
			return "", false
		}
		c.Boosts = append(c.Boosts, catalogdomain.Boost{
			Kind: catalogdomain.BoostCondition, Condition: cond, Weight: w * weight[AttrCondition],
		})
		return label, true
	case AttrPlace:
		if v.Place == nil {
			return "", false
		}
		p, ok := v.Place(value)
		if !ok {
			return "", false
		}
		c.Boosts = append(c.Boosts, ringsAround(p, w)...)
		return value, true
	case AttrNearMe:
		if origin == nil {
			return "", false
		}
		c.Boosts = append(c.Boosts, ringsAround(*origin, w)...)
		return "Gần bạn", true
	case AttrFresh:
		c.Boosts = append(c.Boosts, freshBoosts(now, w)...)
		return "Mới đăng", true
	case AttrOffers:
		c.Boosts = append(c.Boosts, catalogdomain.Boost{Kind: catalogdomain.BoostOffers, Weight: w * weight[AttrOffers]})
		return "Cho trả giá", true
	default:
		return "", false
	}
}

func ringsAround(p catalogdomain.Point, scale float64) []catalogdomain.Boost {
	out := make([]catalogdomain.Boost, 0, len(rings))
	for _, r := range rings {
		out = append(out, catalogdomain.Boost{Kind: catalogdomain.BoostNear, Near: p, RadiusKm: r.km, Weight: scale * r.weight})
	}
	return out
}

func freshBoosts(now time.Time, scale float64) []catalogdomain.Boost {
	out := make([]catalogdomain.Boost, 0, len(freshness))
	for _, f := range freshness {
		out = append(out, catalogdomain.Boost{
			Kind: catalogdomain.BoostFresh, Since: now.Add(-f.age), Weight: scale * f.share * weight[AttrFresh],
		})
	}
	return out
}

// NearbyBoosts are the nearby feed's whole score: the distance rings around
// origin and the freshness boosts.
func NearbyBoosts(origin catalogdomain.Point, now time.Time) []catalogdomain.Boost {
	return append(ringsAround(origin, 1), freshBoosts(now, 1)...)
}

// priceRange reads "lo-hi" in đồng, either side empty, at least one set.
func priceRange(s string) (lo, hi int64, ok bool) {
	a, b, found := strings.Cut(s, "-")
	if !found || (a == "" && b == "") {
		return 0, 0, false
	}
	var err error
	if a != "" {
		if lo, err = strconv.ParseInt(a, 10, 64); err != nil || lo < 0 {
			return 0, 0, false
		}
	}
	if b != "" {
		if hi, err = strconv.ParseInt(b, 10, 64); err != nil || hi <= 0 {
			return 0, 0, false
		}
	}
	if hi > 0 && lo > hi {
		return 0, 0, false
	}
	return lo, hi, true
}

// priceLabel writes a range as a chip says it: "Dưới 3 triệu", "1–2 triệu",
// "Từ 5 triệu".
func priceLabel(lo, hi int64) string {
	switch {
	case lo == 0:
		return "Dưới " + money(hi)
	case hi == 0:
		return "Từ " + money(lo)
	case unit(lo) == unit(hi):
		return number(lo) + "–" + money(hi)
	default:
		return money(lo) + "–" + money(hi)
	}
}

func unit(n int64) string {
	if n >= 1_000_000 {
		return "triệu"
	}
	return "nghìn"
}

func number(n int64) string {
	if n >= 1_000_000 {
		return strings.Replace(strconv.FormatFloat(float64(n)/1_000_000, 'f', -1, 64), ".", ",", 1)
	}
	return strconv.FormatInt(n/1000, 10)
}

func money(n int64) string { return number(n) + " " + unit(n) }

// QueryKey is a query as the cache keys it: lowercase, whitespace collapsed.
func QueryKey(q string) string { return strings.ToLower(strings.Join(strings.Fields(q), " ")) }

// Schema is the JSON schema the model's answer must fit, in strict mode:
// every property required and no other allowed.
func Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["understood","boosts","demotes"],
"properties":{"understood":{"type":"string"},
"boosts":{"type":"array","maxItems":8,"items":{"$ref":"#/$defs/signal"}},
"demotes":{"type":"array","maxItems":4,"items":{"$ref":"#/$defs/signal"}}},
"$defs":{"signal":{"type":"object","additionalProperties":false,"required":["attr","values"],
"properties":{"attr":{"type":"string","enum":["phrase","category","price","condition","place","near_me","fresh","offers"]},
"values":{"type":"array","maxItems":3,"items":{"type":"string"}}}}}}`)
}

// SystemPrompt is the model's instructions, with the category tree and
// today's date.
func SystemPrompt(categories []catalogdomain.Category, today time.Time) string {
	var b strings.Builder
	b.WriteString(`You read a search query typed on ShopNexus, a Vietnamese second-hand marketplace, and say what the
search should prefer (boosts) and avoid (demotes). Each signal names an attribute and its values in priority order,
at most three. Attributes:
- phrase: search words rewritten in correct Vietnamese with diacritics, abbreviations expanded ("dt" is "điện thoại",
  "xe dap" is "xe đạp"), brand and model names kept.
- category: codes from the list below, and only those; a subcategory code when the query is specific.
- price: "lo-hi" in whole đồng, either side empty ("duoi 5tr" is "-5000000", "1-2 trieu" is "1000000-2000000").
- condition: new, like_new or used.
- place: a province or commune name the person mentions, as Vietnamese spells it.
- near_me: the person wants listings near them; no values.
- fresh: the person wants recent listings ("mới đăng"); no values.
- offers: the person wants to bargain ("thương lượng", "trả giá"); no values.
Put an accessory or a related item in demotes when the person is clearly looking for the main item, and the other way
round. Never write a weight or a score. understood is one short Vietnamese sentence of what will be searched.

Categories:
`)
	for _, c := range categories {
		b.WriteString(c.Code + " — " + c.DisplayName + "\n")
	}
	b.WriteString("\nToday is " + today.Format("2006-01-02") + ".\n")
	return b.String()
}
