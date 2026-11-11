package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/assist/domain"
)

func TestParseLink(t *testing.T) {
	t.Parallel()
	want := domain.Link{Site: "chotot.com", ListID: 119482736, URL: "https://www.chotot.com/119482736.htm"}
	for _, s := range []string{
		"https://www.chotot.com/tp-ho-chi-minh/quan-binh-thanh/mua-ban-dien-thoai/119482736.htm",
		"  https://www.chotot.com/119482736.htm  ",
		"http://chotot.com/119482736.htm",
		"https://m.chotot.com/mua-ban-dien-thoai/119482736.htm?utm_source=share&utm_medium=app",
		"https://xe.chotot.com/mua-ban-xe-may/119482736.htm#xem-them",
		"https://WWW.CHOTOT.COM/119482736.htm",
		"https://www.chotot.com./119482736",
		"https://www.chotot.com/mua-ban/119482736/",
	} {
		got, err := domain.ParseLink(s)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v", s, got, err)
		}
	}
	for _, s := range []string{"", "iphone 11 trắng", "chotot.com/119482736.htm", "ftp://chotot.com/1.htm", "https://", "https://" + strings.Repeat("a", 2050)} {
		if _, err := domain.ParseLink(s); !errors.Is(err, domain.ErrNotALink) {
			t.Errorf("%q: %v, want not_a_link", s, err)
		}
	}
	for _, s := range []string{
		"https://www.facebook.com/groups/passdophcm/posts/2048817356",
		"https://chotot.com.evil.io/119482736.htm",
		"https://nhatot.com/119482736.htm",
		"https://notchotot.com/119482736.htm",
		"https://www.chotot.com/mua-ban-dien-thoai",
		"https://www.chotot.com/",
		"https://www.chotot.com/0.htm",
		"https://www.chotot.com/99999999999999999999.htm",
	} {
		if _, err := domain.ParseLink(s); !errors.Is(err, domain.ErrUnsupportedSource) {
			t.Errorf("%q: %v, want unsupported_source", s, err)
		}
	}
}
