package domain

import (
	"embed"

	"github.com/shopnexus/server/internal/platform/locale"
)

//go:embed text/*.json
var textFiles embed.FS

// words is notification's text, pushes, the list and mail, in every
// language: text/<tag>.json.
var words = locale.MustLoad(textFiles, "text")

// pen writes text in one language.
type pen struct{ tag locale.Tag }

func penFor(tag locale.Tag) pen { return pen{locale.Of(string(tag))} }

func (w pen) say(key string, args ...any) string { return words.Say(w.tag, key, args...) }

// name is a person's display name, or what the apps call someone without
// one.
func (w pen) name(s string) string {
	if s == "" {
		return w.say("unnamed")
	}
	return s
}

// money writes an amount the way the app does: "1,2 tr" from a million,
// "350.000 ₫" below it.
func (w pen) money(n int64) string {
	if n >= 1_000_000 {
		return w.say("money.million", locale.Decimal(w.tag, float64(n)/1_000_000))
	}
	return locale.Money(w.tag, n)
}

// named is a name kept with a notice: someone without one keeps the
// Vietnamese placeholder, as notices stored before languages did.
func named(name string) string { return penFor(locale.Vietnamese).name(name) }
