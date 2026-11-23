package domain

import (
	"embed"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/locale"
)

//go:embed text/*.json
var textFiles embed.FS

// words is discovery's text, the chip labels, in every language:
// text/<tag>.json.
var words = locale.MustLoad(textFiles, "text")

// writer writes chip labels in one language.
type writer struct{ tag locale.Tag }

func (w writer) say(key string, args ...any) string { return words.Say(w.tag, key, args...) }

// condition is a condition chip's label; false for an unknown condition.
func (w writer) condition(c catalogdomain.Condition) (string, bool) {
	switch c {
	case catalogdomain.ConditionNew:
		return w.say("chip.condition.new"), true
	case catalogdomain.ConditionLikeNew:
		return w.say("chip.condition.like_new"), true
	case catalogdomain.ConditionUsed:
		return w.say("chip.condition.used"), true
	default:
		return "", false
	}
}

// price writes a range as a chip says it: "Dưới 3 triệu", "1–2 triệu",
// "Từ 5 triệu"; "Under 3M ₫", "1–2M ₫", "From 5M ₫".
func (w writer) price(lo, hi int64) string {
	switch {
	case lo == 0:
		return w.say("chip.price.under", w.money(hi))
	case hi == 0:
		return w.say("chip.price.from", w.money(lo))
	case lo >= 1_000_000 == (hi >= 1_000_000):
		return w.say("chip.price.between", w.number(lo), w.money(hi))
	default:
		return w.say("chip.price.between", w.bare(lo), w.money(hi))
	}
}

// money is an amount in millions or thousands with its unit.
func (w writer) money(n int64) string {
	if n >= 1_000_000 {
		return w.say("chip.price.million", w.number(n))
	}
	return w.say("chip.price.thousand", w.number(n))
}

// bare is money without the currency, for the low end of a range whose ends
// differ in unit.
func (w writer) bare(n int64) string {
	if n >= 1_000_000 {
		return w.say("chip.price.million_bare", w.number(n))
	}
	return w.say("chip.price.thousand_bare", w.number(n))
}

// number is n in millions from a million, else in whole thousands.
func (w writer) number(n int64) string {
	if n >= 1_000_000 {
		return locale.Decimal(w.tag, float64(n)/1_000_000)
	}
	return locale.Decimal(w.tag, float64(n/1000))
}
