// Package corpus holds the hand-written texts seed builders draw from:
// reviews, replies, bios and names (README.md has the schema and the
// sampling rules). Every draw takes a number in [0, 1), so a builder that
// derives it from a key gets the same pack each run.
package corpus

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed *.json
var files embed.FS

// Review is one review text.
type Review struct {
	Role  string `json:"role"`
	Stars int    `json:"stars"`
	Cat   string `json:"cat"`
	Text  string `json:"text"`
}

// Reply is one reply text.
type Reply struct {
	Context string `json:"context"`
	Role    string `json:"role"`
	Text    string `json:"text"`
}

// Bio is one profile bio.
type Bio struct {
	Persona string `json:"persona"`
	Text    string `json:"text"`
}

type weighted struct {
	Name    string  `json:"name"`
	Pattern string  `json:"pattern"`
	G       string  `json:"g"`
	W       float64 `json:"w"`
}

// Corpus is every file, read once.
type Corpus struct {
	Reviews   []Review
	Replies   []Reply
	Bios      []Bio
	EmptyBio  float64
	family    []weighted
	given     []weighted
	middle    map[string][]weighted
	patterns  []weighted
	nicknames []weighted
}

// Load reads the embedded files.
func Load() (*Corpus, error) {
	var c Corpus
	var reviews struct{ Texts []Review }
	var replies struct{ Texts []Reply }
	var bios struct {
		EmptyRate float64 `json:"empty_rate"`
		Bios      []Bio
	}
	var names struct {
		Family    []weighted
		Given     []weighted
		Middle    map[string][]weighted
		Patterns  []weighted `json:"display_patterns"`
		Nicknames []weighted
	}
	for name, v := range map[string]any{"reviews.json": &reviews, "replies.json": &replies, "bios.json": &bios, "names.json": &names} {
		raw, err := files.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if err = json.Unmarshal(raw, v); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	c.Reviews, c.Replies, c.Bios, c.EmptyBio = reviews.Texts, replies.Texts, bios.Bios, bios.EmptyRate
	c.family, c.given, c.middle, c.patterns, c.nicknames = names.Family, names.Given, names.Middle, names.Patterns, names.Nicknames
	return &c, nil
}

// weight is w's weight, 1 when the file sets none.
func (w weighted) weight() float64 {
	if w.W <= 0 {
		return 1
	}
	return w.W
}

// pick draws by weight.
func pick(ws []weighted, u float64) weighted {
	total := 0.0
	for _, w := range ws {
		total += w.weight()
	}
	at := u * total
	for _, w := range ws {
		if at -= w.weight(); at < 0 {
			return w
		}
	}
	return ws[len(ws)-1]
}

// Name draws a display name for gender g ("f" or "m") from u, five draws.
func (c *Corpus) Name(g string, u [5]float64) string {
	of := func(ws []weighted) []weighted {
		var out []weighted
		for _, w := range ws {
			if w.G == "" || w.G == "u" || w.G == g {
				out = append(out, w)
			}
		}
		return out
	}
	family, given := pick(c.family, u[1]).Name, pick(of(c.given), u[2]).Name
	middle := ""
	if ms := c.middle[g]; len(ms) > 0 {
		middle = pick(ms, u[3]).Name
	}
	r := strings.NewReplacer(
		"{family}", family, "{given}", given, "{middle}", middle, "{nickname}", pick(of(c.nicknames), u[4]).Name,
		"{family_ascii}", ASCII(family), "{given_ascii}", ASCII(given), "{given_lower}", strings.ToLower(given),
	)
	return strings.Join(strings.Fields(r.Replace(pick(c.patterns, u[0]).Pattern)), " ")
}

// Stars draws a review's stars for role, by the README's weights.
func Stars(role string, u float64) int {
	w := [5]float64{0.62, 0.20, 0.09, 0.04, 0.05}
	if role == "buyer" {
		w = [5]float64{0.70, 0.17, 0.06, 0.03, 0.04}
	}
	for i, x := range w {
		if u < x {
			return 5 - i
		}
		u -= x
	}
	return 1
}

// ReviewText draws a text for a review of role and stars on a listing under
// root: about a quarter empty, else half the time one about the root.
func (c *Corpus) ReviewText(role string, stars int, root string, u [3]float64) string {
	if u[0] < 0.25 {
		return ""
	}
	var same, general []string
	for _, r := range c.Reviews {
		if r.Role != role || r.Stars != stars {
			continue
		}
		switch r.Cat {
		case root:
			same = append(same, r.Text)
		case "any":
			general = append(general, r.Text)
		default:
		}
	}
	list := general
	if len(same) > 0 && (u[1] < 0.5 || len(general) == 0) {
		list = same
	}
	if len(list) == 0 {
		return ""
	}
	return list[int(u[2]*float64(len(list)))]
}

// ReplyText draws whether the person reviewed replies to a review of stars,
// and what: about 60% of 1 and 2 star reviews and 30% of the others.
func (c *Corpus) ReplyText(role string, stars int, u [2]float64) (string, bool) {
	context, rate := "positive", 0.3
	switch {
	case stars <= 2:
		context, rate = "negative", 0.6
	case stars == 3:
		context = "neutral"
	default:
	}
	if u[0] >= rate {
		return "", false
	}
	var list []string
	for _, r := range c.Replies {
		if r.Context == context && (r.Role == role || r.Role == "any") {
			list = append(list, r.Text)
		}
	}
	if len(list) == 0 {
		return "", false
	}
	return list[int(u[1]*float64(len(list)))], true
}

// BioText draws a bio for persona, empty at the corpus's rate.
func (c *Corpus) BioText(persona string, u [2]float64) string {
	if u[0] < c.EmptyBio {
		return ""
	}
	var list []string
	for _, b := range c.Bios {
		if b.Persona == persona {
			list = append(list, b.Text)
		}
	}
	if len(list) == 0 {
		return ""
	}
	return list[int(u[1]*float64(len(list)))]
}

var ascii = strings.NewReplacer(
	"à", "a", "á", "a", "ả", "a", "ã", "a", "ạ", "a", "ă", "a", "ằ", "a", "ắ", "a", "ẳ", "a", "ẵ", "a", "ặ", "a",
	"â", "a", "ầ", "a", "ấ", "a", "ẩ", "a", "ẫ", "a", "ậ", "a", "è", "e", "é", "e", "ẻ", "e", "ẽ", "e", "ẹ", "e",
	"ê", "e", "ề", "e", "ế", "e", "ể", "e", "ễ", "e", "ệ", "e", "ì", "i", "í", "i", "ỉ", "i", "ĩ", "i", "ị", "i",
	"ò", "o", "ó", "o", "ỏ", "o", "õ", "o", "ọ", "o", "ô", "o", "ồ", "o", "ố", "o", "ổ", "o", "ỗ", "o", "ộ", "o",
	"ơ", "o", "ờ", "o", "ớ", "o", "ở", "o", "ỡ", "o", "ợ", "o", "ù", "u", "ú", "u", "ủ", "u", "ũ", "u", "ụ", "u",
	"ư", "u", "ừ", "u", "ứ", "u", "ử", "u", "ữ", "u", "ự", "u", "ỳ", "y", "ý", "y", "ỷ", "y", "ỹ", "y", "ỵ", "y",
	"đ", "d", "Đ", "D", "Ă", "A", "Â", "A", "Ê", "E", "Ô", "O", "Ơ", "O", "Ư", "U", "Á", "A", "À", "A", "Ó", "O",
	"Ú", "U", "Í", "I", "É", "E", "Ý", "Y",
)

// ASCII drops a Vietnamese name's diacritics, Đ as D.
func ASCII(s string) string { return ascii.Replace(s) }
