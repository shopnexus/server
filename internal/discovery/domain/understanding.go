package domain

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/locale"
)

// PromptVersion names the prompt and schema below; a change to either bumps
// it, so cached answers of the old prompt are no longer read.
// 2 added rewritten and visual (2026-11-09); 3 is the same prompt read by
// Gemini 2.5 Flash in place of Claude Haiku (2026-11-23); 4 says the
// marketplace sells new and used items (2026-11-28).
const PromptVersion = 4

const (
	// ModelTimeout bounds one model call, which a search waits for, and a
	// search's embeddings.
	ModelTimeout = 4 * time.Second
	// CacheTTL is how long an answer is reused for the same query.
	CacheTTL = 7 * 24 * time.Hour
	// VectorKeepUnused is how long a text's vector stays after its write when
	// no search reads it again, VectorKeepUsed after its last read when one
	// did; past VectorCacheMax rows the least used go first. A read counts
	// once per VectorTouchEvery, so most reads write nothing.
	VectorKeepUnused = 30 * 24 * time.Hour
	VectorKeepUsed   = 180 * 24 * time.Hour
	VectorCacheMax   = 2_000_000
	VectorTouchEvery = 24 * time.Hour
	// ModelCallsPerDay caps the model's reads of one person's queries on a
	// UTC day; a cached answer does not count. Past it the raw words search.
	ModelCallsPerDay = 200
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
	// Understood is a sentence the model writes before the other fields, so
	// it reads the whole query first; no client gets it.
	Understood string `json:"understood"`
	// Rewritten is the query in correct Vietnamese, for the text vectors.
	Rewritten string `json:"rewritten"`
	// Visual is how the item looks, in short English, for the photo
	// vectors; empty when the query says nothing about looks.
	Visual  string   `json:"visual"`
	Boosts  []Signal `json:"boosts"`
	Demotes []Signal `json:"demotes"`
}

// Vocabulary resolves what the model named against real rows, and says the
// chips in a language.
type Vocabulary struct {
	// Categories maps each code, roots and subcategories, to its Vietnamese
	// name, the one phrases and the search history compare.
	Categories map[string]string
	// Labels maps codes to the names chips show in Locale; a code it lacks
	// shows its Vietnamese name.
	Labels map[string]string
	// Locale is the language of the chips; empty reads as the default.
	Locale locale.Tag
	// Places are the points of the place names in the understanding, as
	// PlaceNames gives them; a name with no single place is missing.
	Places map[string]catalogdomain.Point
}

// Chip is a signal shown to the person, who may turn it off by its ID.
type Chip struct {
	ID    string `json:"id"`
	Kind  Attr   `json:"kind"`
	Label string `json:"label"`
	// Code is a category chip's category code, which the search history
	// keeps; the API leaves it out.
	Code string `json:"-"`
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

// Compile resolves u into phrases and boosts. A value that does not resolve
// is dropped: a page missing one signal is still worth serving. raw, the
// person's own words, is always a phrase unless the model named it. A chip
// in off is left out with its terms. origin is where the person searches
// from, nil when they gave no position.
func Compile(u Understanding, raw string, v Vocabulary, origin *catalogdomain.Point, now time.Time, off []string) Compiled {
	var c Compiled
	twin := twins(u, v)
	for _, group := range []struct {
		signals []Signal
		sign    float64
	}{{u.Boosts, 1}, {u.Demotes, -1}} {
		mark := "+"
		if group.sign < 0 {
			mark = "-"
		}
		for _, s := range group.signals {
			values := s.Values
			if len(values) == 0 && flag(s.Attr) {
				values = []string{""}
			}
			for i, value := range values {
				if i >= len(position) {
					break
				}
				term := mark + string(s.Attr)
				if value != "" {
					term += "=" + value
				}
				c.Asked = append(c.Asked, term)
				id := fmt.Sprintf("%s-%d", s.Attr, i)
				first, isTwin := twin[id]
				if group.sign > 0 && (slices.Contains(off, id) || isTwin && slices.Contains(off, first)) {
					continue
				}
				w := group.sign * position[i]
				label, ok := c.add(s.Attr, strings.TrimSpace(value), w, v, origin, now)
				if !ok {
					continue
				}
				c.Ran = append(c.Ran, fmt.Sprintf("%s w=%+.2f", term, w))
				if group.sign > 0 && !isTwin {
					chip := Chip{ID: id, Kind: s.Attr, Label: label}
					if s.Attr == AttrCategory {
						chip.Code = strings.TrimSpace(value)
					}
					c.Chips = append(c.Chips, chip)
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

// twins maps a boost's chip ID to an earlier boost chip with the same label,
// as when the model names "iPhone" as a category and as a phrase. Only the
// first shows, and turning it off drops both.
func twins(u Understanding, v Vocabulary) map[string]string {
	first := map[string]string{}
	out := map[string]string{}
	for _, s := range u.Boosts {
		for i, value := range s.Values[:min(len(s.Values), len(position))] {
			value = strings.TrimSpace(value)
			label := value
			switch s.Attr {
			case AttrPhrase:
			case AttrCategory:
				label = v.Categories[value]
			case AttrPrice, AttrCondition, AttrPlace, AttrNearMe, AttrFresh, AttrOffers:
				continue
			default:
				continue
			}
			if label == "" {
				continue
			}
			id, key := fmt.Sprintf("%s-%d", s.Attr, i), QueryKey(label)
			if f, ok := first[key]; ok {
				out[id] = f
			} else {
				first[key] = id
			}
		}
	}
	return out
}

// PlaceNames are the place values of u that Compile looks up in
// Vocabulary.Places.
func PlaceNames(u Understanding) []string {
	var out []string
	for _, s := range slices.Concat(u.Boosts, u.Demotes) {
		if s.Attr != AttrPlace {
			continue
		}
		for _, value := range s.Values[:min(len(s.Values), len(position))] {
			if name := strings.TrimSpace(value); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// flag reports an attribute that takes no value.
func flag(a Attr) bool { return a == AttrNearMe || a == AttrFresh || a == AttrOffers }

// add turns one value into terms at scale w (the sign times the position)
// and returns its chip's label, or false when it does not resolve.
func (c *Compiled) add(a Attr, value string, w float64, v Vocabulary, origin *catalogdomain.Point, now time.Time) (string, bool) {
	say := writer{locale.Of(string(v.Locale))}
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
		if label, ok := v.Labels[value]; ok {
			name = label
		}
		return name, true
	case AttrPrice:
		lo, hi, ok := priceRange(value)
		if !ok {
			return "", false
		}
		c.Boosts = append(c.Boosts, catalogdomain.Boost{
			Kind: catalogdomain.BoostPrice, PriceMin: lo, PriceMax: hi, Weight: w * weight[AttrPrice],
		})
		return say.price(lo, hi), true
	case AttrCondition:
		cond := catalogdomain.Condition(value)
		label, ok := say.condition(cond)
		if !ok {
			return "", false
		}
		c.Boosts = append(c.Boosts, catalogdomain.Boost{
			Kind: catalogdomain.BoostCondition, Condition: cond, Weight: w * weight[AttrCondition],
		})
		return label, true
	case AttrPlace:
		p, ok := v.Places[value]
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
		return say.say("chip.near_me"), true
	case AttrFresh:
		c.Boosts = append(c.Boosts, freshBoosts(now, w)...)
		return say.say("chip.fresh"), true
	case AttrOffers:
		c.Boosts = append(c.Boosts, catalogdomain.Boost{Kind: catalogdomain.BoostOffers, Weight: w * weight[AttrOffers]})
		return say.say("chip.offers"), true
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

// QueryKey is a query as the cache keys it: lowercase, whitespace collapsed.
func QueryKey(q string) string { return strings.ToLower(strings.Join(strings.Fields(q), " ")) }

// Schema is the JSON schema the model's answer must fit, in strict mode:
// every property required and no other allowed.
func Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["understood","rewritten","visual","boosts","demotes"],
"properties":{"understood":{"type":"string"},"rewritten":{"type":"string"},"visual":{"type":"string"},
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
	b.WriteString(`You read a search query typed on ShopNexus, a Vietnamese marketplace where people buy and sell new and
used items to each other, and say what the search should prefer (boosts) and avoid (demotes). Each signal names an
attribute and its values in priority order, at most three. Attributes:
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
rewritten is the whole query in correct Vietnamese with diacritics, typos fixed and abbreviations spelled out
("ip13 pm xanh" is "iPhone 13 Pro Max màu xanh"). visual describes in English, in at most 12 words, how the item
the person wants looks ("iPhone 13 Pro Max smartphone, blue"); leave it empty when the query only names a price, a
place, a condition or a time.

Categories:
`)
	for _, c := range categories {
		b.WriteString(c.Code + " — " + c.DisplayName + "\n")
	}
	b.WriteString("\nToday is " + today.Format("2006-01-02") + ".\n")
	return b.String()
}
