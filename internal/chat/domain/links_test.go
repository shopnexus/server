package domain_test

import (
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
)

func TestLinks(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("01a1f350-8d9a-7ba9-bc13-0b1b65108e7d")
	hosts := []string{"shopnexus.id.vn", "localhost"}
	cases := []struct {
		name, text string
		want       []domain.Link
	}{
		{
			"listing", "xem https://shopnexus.id.vn/listings/" + id.String() + " nhé",
			[]domain.Link{{URL: "https://shopnexus.id.vn/listings/" + id.String(), Kind: domain.LinkListing, ID: id}},
		},
		{
			"www and a full stop", "https://www.shopnexus.id.vn/sellers/" + id.String() + ".",
			[]domain.Link{{URL: "https://www.shopnexus.id.vn/sellers/" + id.String(), Kind: domain.LinkSeller, ID: id}},
		},
		{
			"app link", "shopnexus://order/" + id.String(),
			[]domain.Link{{URL: "shopnexus://order/" + id.String(), Kind: domain.LinkOrder, ID: id}},
		},
		{
			"category", "http://localhost:4321/categories/phones.iphone",
			[]domain.Link{{URL: "http://localhost:4321/categories/phones.iphone", Kind: domain.LinkCategory, Code: "phones.iphone"}},
		},
		{
			"search", "https://shopnexus.id.vn/search?q=b%E1%BA%A3ng+t%C3%AAn",
			[]domain.Link{{URL: "https://shopnexus.id.vn/search?q=b%E1%BA%A3ng+t%C3%AAn", Kind: domain.LinkSearch, Query: "bảng tên"}},
		},
		{"another site", "https://example.com/listings/" + id.String(), nil},
		{"a page with no card", "https://shopnexus.id.vn/help", nil},
		{"not an id", "https://shopnexus.id.vn/listings/abc", nil},
		{"an empty search", "https://shopnexus.id.vn/search?q=", nil},
		{
			"twice counts once", "https://shopnexus.id.vn/listings/" + id.String() + " https://shopnexus.id.vn/listings/" + id.String(),
			[]domain.Link{{URL: "https://shopnexus.id.vn/listings/" + id.String(), Kind: domain.LinkListing, ID: id}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := domain.Links(c.text, hosts)
			if len(got) != len(c.want) {
				t.Fatalf("Links(%q) = %+v, want %+v", c.text, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("Links(%q)[%d] = %+v, want %+v", c.text, i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestLinksStopsAtMax(t *testing.T) {
	t.Parallel()
	var text strings.Builder
	for range domain.MaxLinks + 2 {
		text.WriteString("https://shopnexus.id.vn/listings/" + uuid.New().String() + " ")
	}
	if got := domain.Links(text.String(), []string{"shopnexus.id.vn"}); len(got) != domain.MaxLinks {
		t.Fatalf("got %d links, want %d", len(got), domain.MaxLinks)
	}
}
