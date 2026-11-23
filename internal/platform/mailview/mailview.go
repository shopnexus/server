// Package mailview lays out the mail the server sends: a card with a
// drawing, a title, the blocks the mail needs and one action, as drawn in
// .superdesign/mockups. Callers fill a Letter and send it through a Mailer,
// which renders it as HTML with its images inline, and as plain text.
package mailview

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
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

// Letter is one mail, written in Locale; the layout's own words follow it.
// Paths are on the website the Mailer links to.
type Letter struct {
	Locale    locale.Tag
	Subject   string
	Preheader string
	Group     string
	Art       Art
	Title     string
	Lead      string
	// Code is a six-digit code, shown large over CodeNote.
	Code, CodeNote string
	// Stars links five stars to Stars?stars=<n>#review, a page whose
	// review form opens with that many stars picked.
	Stars string
	Item  *Item
	Steps *Steps
	Rows  []Row
	Note  string
	// Action is the one button.
	Action *Action
	Why    string
	// Footer is a link under Why, such as the settings that stop the mail.
	Footer *Action
}

// Item is the listing a mail is about.
type Item struct {
	Photo, Title, Price string
	// Side names the other party, as "Người bán · Hoàng Nam".
	Side string
}

// Steps is how far something got: the steps before At carry a check, At
// a ring.
type Steps struct {
	Names []string
	At    int
}

// Row is a fact, label at left and value at right.
type Row struct {
	Label, Value string
	Mono, Strong bool
}

// Action is a link: a label and a path on the site.
type Action struct{ Label, Path string }

// Mailer renders letters with links on Site, the website's origin, and
// sends them through Email. A Mailer without a Site writes no links.
type Mailer struct {
	Email email.Sender
	Site  string
}

//go:embed letter.html art/*.png locales/*.json
var files embed.FS

var (
	page  = template.Must(template.ParseFS(files, "letter.html"))
	words = locale.MustLoad(files, "locales")
	// art holds every drawing and the logo, read once.
	art = func() map[Art][]byte {
		m := map[Art][]byte{}
		for _, a := range []Art{"logo", ArtVerified, ArtOrders, ArtShipped, ArtReceived, ArtDeal, ArtNotFound, ArtSearch, ArtOffline, ArtPosted} {
			b, err := files.ReadFile("art/" + string(a) + ".png")
			if err != nil {
				panic(err)
			}
			m[a] = b
		}
		return m
	}()
)

type step struct {
	Name                   string
	Done, Current, Reached bool
	First, Last            bool
}

type view struct {
	Letter

	Site      string
	Help      string
	Tagline   string
	CodeHead  string
	CodeTail  string
	StepList  []step
	StarList  []string
	ActionURL string
	FooterURL string
}

// Send renders l and mails it to one address; key names the mail at the
// provider. An email.ErrRefused passes through.
func (m Mailer) Send(ctx context.Context, to, key string, l Letter) error {
	msg, err := m.Message(to, key, l)
	if err != nil {
		return err
	}
	if err = m.Email.Send(ctx, msg); err != nil {
		return fmt.Errorf("send %q: %w", l.Subject, err)
	}
	return nil
}

// Message renders l for one address.
func (m Mailer) Message(to, key string, l Letter) (email.Message, error) {
	l.Locale = locale.Of(string(l.Locale))
	v := view{Letter: l, Site: m.Site, Help: words.Say(l.Locale, "help"), Tagline: words.Say(l.Locale, "tagline")}
	if len(l.Code) == 6 {
		v.CodeHead, v.CodeTail = l.Code[:3], l.Code[3:]
	}
	if s := l.Steps; s != nil {
		for i, name := range s.Names {
			v.StepList = append(v.StepList, step{
				Name: name, Done: i < s.At, Current: i == s.At, Reached: i <= s.At, First: i == 0, Last: i == len(s.Names)-1,
			})
		}
	}
	if m.Site != "" {
		if l.Action != nil {
			v.ActionURL = m.Site + l.Action.Path
		}
		if l.Footer != nil {
			v.FooterURL = m.Site + l.Footer.Path
		}
		if l.Stars != "" {
			for n := 1; n <= 5; n++ {
				v.StarList = append(v.StarList, m.Site+l.Stars+"?stars="+strconv.Itoa(n)+"#review")
			}
		}
	}
	var html bytes.Buffer
	if err := page.Execute(&html, v); err != nil {
		return email.Message{}, fmt.Errorf("render %q: %w", l.Subject, err)
	}
	drawing, ok := art[l.Art]
	if !ok {
		return email.Message{}, fmt.Errorf("no drawing %q", l.Art)
	}
	return email.Message{
		To: to, Subject: l.Subject, Text: v.text(), HTML: html.String(), Key: key,
		Inline: []email.Inline{{CID: "logo", Name: "logo.png", Content: art["logo"]}, {CID: "art", Name: string(l.Art) + ".png", Content: drawing}},
	}, nil
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
		b.WriteString("\n★ " + v.StarList[len(v.StarList)-1] + "\n")
	}
	if v.ActionURL != "" {
		b.WriteString("\n" + v.Action.Label + ": " + v.ActionURL + "\n")
	}
	b.WriteString("\n" + v.Why + "\n")
	if v.FooterURL != "" {
		b.WriteString(v.Footer.Label + ": " + v.FooterURL + "\n")
	}
	return b.String()
}
