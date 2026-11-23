// Package locale names the languages the server writes text in, picks one
// from a request's Accept-Language, and writes amounts and dates in it.
package locale

import (
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Tag is a language the server writes in, as a BCP 47 base tag.
type Tag string

const (
	Vietnamese Tag = "vi"
	English    Tag = "en"
)

// Supported lists the languages; the first is the default.
var Supported = []Tag{Vietnamese, English}

var matcher = func() language.Matcher {
	tags := make([]language.Tag, len(Supported))
	for i, t := range Supported {
		tags[i] = language.Make(string(t))
	}
	return language.NewMatcher(tags)
}()

// Match picks the supported language an Accept-Language header prefers,
// or "" when it names none; Of reads "" as the default.
func Match(acceptLanguage string) Tag {
	tags, _, _ := language.ParseAcceptLanguage(acceptLanguage) // a bad header names no language
	_, i, confidence := matcher.Match(tags...)
	if confidence == language.No {
		return ""
	}
	return Supported[i]
}

// FromRequest is Match on r's Accept-Language.
func FromRequest(r *http.Request) Tag { return Match(r.Header.Get("Accept-Language")) }

// Of reads a stored tag; an unknown or empty one reads as the default.
func Of(s string) Tag {
	if t := Tag(s); slices.Contains(Supported, t) {
		return t
	}
	return Supported[0]
}

// Money writes an amount of đồng in full, grouped as t groups digits:
// "3.800.000 ₫", "3,800,000 ₫".
func Money(t Tag, n int64) string {
	return message.NewPrinter(language.Make(string(t))).Sprintf("%d ₫", n)
}

// Decimal writes f with t's decimal mark and up to three decimals: "1,25",
// "1.25".
func Decimal(t Tag, f float64) string {
	return message.NewPrinter(language.Make(string(t))).Sprint(number.Decimal(f, number.MaxFractionDigits(3)))
}

// dates are the day-and-month layouts, in Vietnam time.
var dates = map[Tag]string{Vietnamese: "2/1", English: "Jan 2"}

var vietnam = time.FixedZone("ICT", 7*60*60)

// Date writes the day and month of at, on Vietnam's clock.
func Date(t Tag, at time.Time) string { return at.In(vietnam).Format(dates[Of(string(t))]) }

// Catalog is one package's text in every supported language: <tag>.json
// files of key → fmt format.
type Catalog map[Tag]map[string]string

// MustLoad reads <tag>.json from dir of fsys for every supported language
// and panics unless each file has the default's keys, no more and no fewer.
func MustLoad(fsys embed.FS, dir string) Catalog {
	c := Catalog{}
	for _, t := range Supported {
		b, err := fsys.ReadFile(dir + "/" + string(t) + ".json")
		if err != nil {
			panic(fmt.Sprintf("locale: read %s: %v", t, err))
		}
		m := map[string]string{}
		if err = json.Unmarshal(b, &m); err != nil {
			panic(fmt.Sprintf("locale: parse %s/%s.json: %v", dir, t, err))
		}
		c[t] = m
	}
	want := slices.Sorted(maps.Keys(c[Supported[0]]))
	for _, t := range Supported[1:] {
		if got := slices.Sorted(maps.Keys(c[t])); !slices.Equal(got, want) {
			panic(fmt.Sprintf("locale: %s/%s.json has keys %s, %s.json %s", dir, t, strings.Join(got, ","), Supported[0], strings.Join(want, ",")))
		}
	}
	return c
}

// Say writes key in t, a supported tag, with args; a key the catalog lacks
// is itself.
func (c Catalog) Say(t Tag, key string, args ...any) string {
	f, ok := c[t][key]
	if !ok {
		return key
	}
	if len(args) == 0 {
		return f
	}
	return fmt.Sprintf(f, args...)
}
