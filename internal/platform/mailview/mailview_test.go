package mailview_test

import (
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/mailview"
)

var mailer = mailview.Mailer{Site: "https://shopnexus.id.vn"}

// A letter shows every block it has, links on the site, both images inline,
// and the same facts as plain text.
func TestLetter(t *testing.T) {
	t.Parallel()
	l := mailview.Letter{
		Subject: "Hoàng Nam đã gửi iPhone X", Group: "Đơn hàng", Art: mailview.ArtShipped, Title: "Hoàng Nam đã gửi hàng",
		Item:   &mailview.Item{Photo: "https://media.test/cover.jpg", Title: "iPhone X <64GB>", Price: "3.800.000 ₫", Side: "Người bán · Hoàng Nam"},
		Steps:  &mailview.Steps{Names: []string{"Chốt giá", "Tạo đơn", "Nhận hàng", "Đánh giá"}, At: 2},
		Rows:   []mailview.Row{{Label: "Mã vận đơn", Value: "S1.A1.17373471", Mono: true}},
		Action: &mailview.Action{Label: "Xem đơn hàng", Path: "/orders/o1"}, Why: "Bạn có đơn hàng.",
		Footer: &mailview.Action{Label: "Tắt email đơn hàng", Path: "/notifications#settings"},
	}
	m, err := mailer.Message("linh@example.com", "ev/1", l)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`src="cid:logo"`, `src="cid:art"`, "iPhone X &lt;64GB&gt;", `href="https://shopnexus.id.vn/orders/o1"`,
		`href="https://shopnexus.id.vn/notifications#settings"`, "Nhận hàng", "S1.A1.17373471", "✓", `lang="vi"`, ">Trợ giúp<",
	} {
		if !strings.Contains(m.HTML, want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
	if strings.Contains(m.HTML, "ZgotmplZ") {
		t.Error("a value was filtered as unsafe")
	}
	if m.To != "linh@example.com" || m.Key != "ev/1" || len(m.Inline) != 2 || m.Inline[1].Name != "cod_placed.png" || len(m.Inline[1].Content) == 0 {
		t.Fatalf("message %+v", m.Inline)
	}
	if !strings.Contains(m.Text, "Xem đơn hàng: https://shopnexus.id.vn/orders/o1") || !strings.Contains(m.Text, "Mã vận đơn: S1.A1.17373471") {
		t.Fatalf("text %q", m.Text)
	}
}

// A code letter shows the code split in two over its note; a Mailer without
// a site writes no links; the layout's words follow the letter's language.
func TestCodeLetterInEnglishWithoutSite(t *testing.T) {
	t.Parallel()
	m, err := mailview.Mailer{}.Message("linh@example.com", "c1", mailview.Letter{
		Locale: locale.English, Subject: "Your code: 583920", Art: mailview.ArtVerified, Title: "Your code",
		Code: "583920", CodeNote: "Expires in 5 minutes", Stars: "/orders/o1", Action: &mailview.Action{Label: "Open", Path: "/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.HTML, "583<span") || !strings.Contains(m.HTML, "</span>920") || strings.Contains(m.HTML, "<a ") ||
		!strings.Contains(m.HTML, "Expires in 5 minutes") || !strings.Contains(m.HTML, `lang="en"`) || !strings.Contains(m.HTML, "check before you pay") {
		t.Fatalf("html %s", m.HTML)
	}
}

// An unknown drawing fails the render.
func TestUnknownArt(t *testing.T) {
	t.Parallel()
	if _, err := mailer.Message("a@b.test", "", mailview.Letter{Art: "nope"}); err == nil {
		t.Fatal("rendered without a drawing")
	}
}
