package mailview_test

import (
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/platform/locale"

	"github.com/shopnexus/server/internal/platform/mailview"
)

// An order mail shows every block it has, links on the site, both images
// inline, and the same facts as plain text.
func TestOrderLetter(t *testing.T) {
	t.Parallel()
	l := mailview.Letter{
		Site: "https://shopnexus.id.vn", Subject: "Hoàng Nam đã gửi iPhone X", Preheader: "GHTK S1.A1.17373471",
		Group: "Đơn hàng", Art: mailview.ArtShipped, Title: "Hoàng Nam đã gửi hàng", Lead: "Xem hàng khi shipper tới.",
		Item:   &mailview.Item{Photo: "https://media.test/cover.jpg", Title: "iPhone X <64GB>", Price: "3.800.000 ₫", Side: "Người bán · Hoàng Nam"},
		Steps:  &mailview.Steps{Done: 2, Current: 2},
		Rows:   []mailview.Row{{Label: "Mã vận đơn", Value: "S1.A1.17373471", Mono: true}},
		Action: &mailview.Action{Label: "Xem đơn hàng", Path: "/orders/o1"}, Why: "Bạn có đơn hàng.", Settings: true,
	}
	m, err := l.Message("linh@example.com", "ev/1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`src="cid:logo"`, `src="cid:art"`, "iPhone X &lt;64GB&gt;", `href="https://shopnexus.id.vn/orders/o1"`,
		`href="https://shopnexus.id.vn/notifications#settings"`, "Nhận hàng", "S1.A1.17373471", "✓",
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

// A code mail shows the code split in two and has no settings link; a
// letter without a site has no links at all.
func TestCodeLetterWithoutSite(t *testing.T) {
	t.Parallel()
	m, err := mailview.Letter{
		Subject: "Mã xác minh ShopNexus: 583920", Group: "Bảo mật", Art: mailview.ArtVerified,
		Title: "Mã đăng nhập ShopNexus", Code: "583920", Note: "Đừng đưa mã cho ai.", Why: "Ai đó vừa yêu cầu mã.",
		Stars: "/orders/o1", Action: &mailview.Action{Label: "Mở", Path: "/"},
	}.Message("linh@example.com", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.HTML, "583<span") || !strings.Contains(m.HTML, "</span>920") || strings.Contains(m.HTML, "<a ") {
		t.Fatalf("html %s", m.HTML)
	}
	if !strings.Contains(m.Text, "583920") {
		t.Fatalf("text %q", m.Text)
	}
}

// Every drawing a letter can name is embedded.
func TestEveryArtIsEmbedded(t *testing.T) {
	t.Parallel()
	for _, a := range []mailview.Art{
		mailview.ArtVerified, mailview.ArtOrders, mailview.ArtShipped, mailview.ArtReceived, mailview.ArtDeal,
		mailview.ArtNotFound, mailview.ArtSearch, mailview.ArtOffline, mailview.ArtPosted,
	} {
		if _, err := (mailview.Letter{Art: a}).Message("a@b.test", ""); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
}

// The layout's own words follow the letter's language.
func TestEnglishLayout(t *testing.T) {
	t.Parallel()
	m, err := mailview.Letter{
		Locale: locale.English, Site: "https://shopnexus.id.vn", Art: mailview.ArtShipped, Title: "Shipped",
		Steps: &mailview.Steps{Done: 2, Current: 2}, Settings: true,
	}.Message("a@b.test", "k")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`lang="en"`, "Received", "Turn off order emails", ">Help<"} {
		if !strings.Contains(m.HTML, want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
}
