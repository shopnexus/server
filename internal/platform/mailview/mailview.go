// Package mailview lays out the mail the server sends: a card with a
// drawing, a title, the blocks the mail needs and one action, as drawn in
// .superdesign/mockups. Callers fill a Letter; Message renders it as HTML
// with its images inline, and as plain text.
package mailview

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/url"
	"strconv"
	"strings"

	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/provider/email"
)

// Art is a drawing of the mascot, by its file in art/.
type Art string

const (
	ArtVerified Art = "verified"
	ArtOrders   Art = "orders"
	ArtShipped  Art = "cod_placed"
	ArtReceived Art = "received"
	ArtDeal     Art = "deal"
	ArtNotFound Art = "not_found"
	ArtSearch   Art = "search"
	ArtOffline  Art = "offline"
	ArtPosted   Art = "posted"
)

// Letter is one mail. Site is the website's origin the links go to; a
// Letter without one has no links. Order mail (Settings) links the
// settings that stop it.
type Letter struct {
	// Locale is the language the letter is written in; the caller writes
	// its own text in it, and the layout's words follow.
	Locale    locale.Tag
	Site      string
	Subject   string
	Preheader string
	Group     string
	Art       Art
	Title     string
	Lead      string
	// Code is a sign-in code, shown large.
	Code string
	// Stars links five stars to Stars?stars=<n>#review, a site path whose
	// review form opens with that many stars picked.
	Stars string
	Item  *Item
	// Steps marks the four steps of a COD order, nil for none.
	Steps *Steps
	Rows  []Row
	Note  string
	// Action is the one button; its Path is on Site.
	Action *Action
	Why    string
	// Settings says the mail is one the notification settings can stop.
	Settings bool
}

// Item is the listing a mail is about.
type Item struct {
	Photo, Title, Price string
	// Side names the other party, as "Người bán · Hoàng Nam".
	Side string
}

// Steps is how far an order got: Done steps carry a check, Current a ring;
// Current is -1 when no step is under way.
type Steps struct{ Done, Current int }

// Row is a fact, label at left and value at right.
type Row struct {
	Label, Value string
	Mono, Strong bool
}

// Action is the button: a label and a path on the site.
type Action struct{ Label, Path string }

//go:embed letter.html art/*.png locales/*.json
var files embed.FS

var (
	page  = template.Must(template.ParseFS(files, "letter.html"))
	words = locale.MustLoad(files, "locales")
)

// phases are the keys of a COD order's steps, as the app's Đơn COD names them.
var phases = [4]string{"steps.agreed", "steps.ordered", "steps.received", "steps.reviewed"}

type step struct {
	Name                   string
	Done, Current, Reached bool
	Left, Right            string // the line's colour either side of the dot
}

type star struct {
	N   int
	URL string
}

type view struct {
	Letter

	Lang               string
	Words              map[string]string
	Logo, Picture      template.URL
	CodeHead, CodeTail string
	StepList           []step
	StarList           []star
	ActionURL          string
	SettingsURL        string
	HelpURL            string
}

const (
	done = "#066A61"
	line = "#E1E6E4"
	none = "transparent"
)

// Message renders l for one address. key names the mail at the provider.
func (l Letter) Message(to, key string) (email.Message, error) {
	tag := locale.Of(string(l.Locale))
	v := view{Letter: l, Lang: string(tag), Words: words[tag], Logo: "cid:logo", Picture: "cid:art"}
	if len(l.Code) == 6 {
		v.CodeHead, v.CodeTail = l.Code[:3], l.Code[3:]
	}
	if l.Steps != nil {
		reached := l.Steps.Current
		if reached < 0 {
			reached = l.Steps.Done - 1
		}
		for i, name := range phases {
			s := step{Name: v.Words[name], Done: i < l.Steps.Done, Current: i == l.Steps.Current, Reached: i <= reached, Left: none, Right: none}
			if i > 0 {
				s.Left = lineColour(i <= reached)
			}
			if i < len(phases)-1 {
				s.Right = lineColour(i < reached)
			}
			v.StepList = append(v.StepList, s)
		}
	}
	if l.Site != "" {
		v.HelpURL = l.Site + "/help"
		if l.Settings {
			v.SettingsURL = l.Site + "/notifications#settings"
		}
		if l.Action != nil {
			v.ActionURL = l.Site + l.Action.Path
		}
		if l.Stars != "" {
			for n := 1; n <= 5; n++ {
				v.StarList = append(v.StarList, star{N: n, URL: l.Site + l.Stars + "?stars=" + strconv.Itoa(n) + "#review"})
			}
		}
	}
	var html bytes.Buffer
	if err := page.Execute(&html, v); err != nil {
		return email.Message{}, fmt.Errorf("render %q: %w", l.Subject, err)
	}
	logo, err := files.ReadFile("art/logo.png")
	if err != nil {
		return email.Message{}, fmt.Errorf("read the logo: %w", err)
	}
	art, err := files.ReadFile("art/" + string(l.Art) + ".png")
	if err != nil {
		return email.Message{}, fmt.Errorf("read art %q: %w", l.Art, err)
	}
	return email.Message{
		To: to, Subject: l.Subject, Text: v.text(), HTML: html.String(), Key: key,
		Inline: []email.Inline{{CID: "logo", Name: "logo.png", Content: logo}, {CID: "art", Name: string(l.Art) + ".png", Content: art}},
	}, nil
}

func lineColour(on bool) string {
	if on {
		return done
	}
	return line
}

// text is the plain-text part: the same facts, top to bottom.
func (v view) text() string {
	var b strings.Builder
	b.WriteString(v.Title + "\n\n" + v.Lead + "\n")
	if v.Code != "" {
		b.WriteString("\n" + v.Code + "\n")
	}
	if v.Item != nil {
		b.WriteString("\n" + v.Item.Title + " · " + v.Item.Price + "\n" + v.Item.Side + "\n")
	}
	for _, r := range v.Rows {
		b.WriteString(r.Label + ": " + r.Value + "\n")
	}
	if v.Note != "" {
		b.WriteString("\n" + v.Note + "\n")
	}
	if len(v.StarList) > 0 {
		b.WriteString("\n" + v.Words["text.review"] + ": " + v.Site + v.Stars + "\n")
	}
	if v.ActionURL != "" {
		b.WriteString("\n" + v.Action.Label + ": " + v.ActionURL + "\n")
	}
	b.WriteString("\n" + v.Why + "\n")
	if v.SettingsURL != "" {
		b.WriteString(v.Words["footer.settings"] + ": " + v.SettingsURL + "\n")
	}
	return b.String()
}

// SearchPath is the site's search for q.
func SearchPath(q string) string { return "/search?q=" + url.QueryEscape(q) }
