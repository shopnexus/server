// Package locale names the languages the server writes text in and picks
// one from a request's Accept-Language.
package locale

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"golang.org/x/text/language"
)

// Tag is a language the server writes in, as a BCP 47 base tag.
type Tag string

const (
	Vietnamese Tag = "vi"
	English    Tag = "en"
)

// Supported lists the languages; the first is the default.
var Supported = []Tag{Vietnamese, English}

var matcher = language.NewMatcher([]language.Tag{language.Vietnamese, language.English})

// Match picks the supported language an Accept-Language header prefers;
// Vietnamese for an empty or unmatched header.
func Match(acceptLanguage string) Tag {
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil || len(tags) == 0 {
		return Vietnamese
	}
	_, i, confidence := matcher.Match(tags...)
	if confidence == language.No {
		return Vietnamese
	}
	return Supported[i]
}

// Of reads a stored tag; an unknown one reads as Vietnamese.
func Of(s string) Tag {
	if t := Tag(s); slices.Contains(Supported, t) {
		return t
	}
	return Vietnamese
}

// Catalog is one package's text in every supported language: <tag>.json
// files of key → fmt format.
type Catalog map[Tag]map[string]string

// Load reads <tag>.json from dir of fsys for every supported language and
// fails unless each file has the default's keys, no more and no fewer.
func Load(fsys embed.FS, dir string) (Catalog, error) {
	c := Catalog{}
	for _, t := range Supported {
		b, err := fs.ReadFile(fsys, dir+"/"+string(t)+".json")
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", t, err)
		}
		m := map[string]string{}
		if err = json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("parse %s: %w", t, err)
		}
		c[t] = m
	}
	want := slices.Sorted(maps.Keys(c[Supported[0]]))
	for _, t := range Supported[1:] {
		if got := slices.Sorted(maps.Keys(c[t])); !slices.Equal(got, want) {
			return nil, fmt.Errorf("%s.json has keys %s, %s.json %s", t, strings.Join(got, ","), Supported[0], strings.Join(want, ","))
		}
	}
	return c, nil
}

// MustLoad is Load for a package variable.
func MustLoad(fsys embed.FS, dir string) Catalog {
	c, err := Load(fsys, dir)
	if err != nil {
		panic(err)
	}
	return c
}

// Say writes key in t with args; an unknown t reads as Vietnamese, and a
// key the catalog lacks is itself.
func (c Catalog) Say(t Tag, key string, args ...any) string {
	f, ok := c[Of(string(t))][key]
	if !ok {
		return key
	}
	if len(args) == 0 {
		return f
	}
	return fmt.Sprintf(f, args...)
}
