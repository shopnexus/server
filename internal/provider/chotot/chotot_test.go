package chotot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/provider/chotot"
)

// gateway serves body with status on the listing's path and records the
// request it got.
func gateway(t *testing.T, status int, body []byte, got *http.Request) chotot.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got != nil {
			*got = *r
		}
		if r.URL.Path != "/v1/public/ad-listing/119482736" {
			http.NotFound(w, r)
			return
		}
		if status == http.StatusFound {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return chotot.Client{GatewayURL: srv.URL, CDNURL: "https://cdn.chotot.com", HTTP: srv.Client()}
}

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/ad.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The item comes through; nothing about the seller does.
func TestAdKeepsTheItem(t *testing.T) {
	t.Parallel()
	var req http.Request
	c := gateway(t, http.StatusOK, fixture(t), &req)
	ad, err := c.Ad(context.Background(), 119482736)
	if err != nil {
		t.Fatal(err)
	}
	if ad.ListID != 119482736 || ad.Category != 5010 || ad.Subject != "iPhone 11 64GB trắng pin 81% zin all" || ad.Price != 4_500_000 ||
		ad.CategoryName != "Điện thoại" || len(ad.Images) != 5 || !strings.HasPrefix(ad.Body, "Máy dùng 3 năm") {
		t.Fatalf("%+v", ad)
	}
	want := []chotot.Param{
		{Label: "Hãng", Value: "Apple"},
		{Label: "Dòng máy", Value: "iPhone 11"},
		{Label: "Dung lượng", Value: "64 GB"},
		{Label: "Màu sắc", Value: "Trắng"},
		{Label: "Tình trạng", Value: "Đã sử dụng (chưa sửa chữa)"},
	}
	if !slices.Equal(ad.Params, want) {
		t.Fatalf("params %+v", ad.Params)
	}
	b, err := json.Marshal(ad)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Nguyễn Văn Mẫu", "090000", "Đường Mẫu", "Phường Mẫu", "Quận Mẫu", "Tp Hồ Chí Minh", "10.7769", "106.7009", "avatar"} {
		if bytes.Contains(b, []byte(s)) {
			t.Errorf("the ad carries %q", s)
		}
	}
	if req.Header.Get("User-Agent") == "" || req.Header.Get("Accept") != "application/json" {
		t.Fatalf("headers %v", req.Header)
	}
}

func TestAdErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"not found", http.StatusNotFound, `{"message":"entity not found"}`, chotot.ErrNotFound},
		{"hidden", http.StatusOK, `{"ad":{"list_id":119482736,"subject":"x","status":"hidden","images":["https://cdn.chotot.com/a.jpg"]}}`, chotot.ErrNotFound},
		{"server error", http.StatusInternalServerError, "", chotot.ErrUnavailable},
		{"blocked", http.StatusForbidden, "", chotot.ErrUnavailable},
		{"rate limited", http.StatusTooManyRequests, "", chotot.ErrUnavailable},
		{"redirect", http.StatusFound, "", chotot.ErrUnavailable},
		{"no subject", http.StatusOK, `{"ad":{"list_id":119482736,"subject":" ","images":["https://cdn.chotot.com/a.jpg"]}}`, chotot.ErrFormat},
		{"no images", http.StatusOK, `{"ad":{"list_id":119482736,"subject":"x","images":[]}}`, chotot.ErrFormat},
		{"another listing", http.StatusOK, `{"ad":{"list_id":1,"subject":"x","images":["https://cdn.chotot.com/a.jpg"]}}`, chotot.ErrFormat},
		{"not json", http.StatusOK, `<html>`, chotot.ErrFormat},
		{"too large", http.StatusOK, `{"ad":{"body":"` + strings.Repeat("a", 2<<20) + `"}}`, chotot.ErrFormat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := gateway(t, tc.status, []byte(tc.body), nil).Ad(context.Background(), 119482736)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	t.Run("down", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		c := chotot.Client{GatewayURL: srv.URL, CDNURL: "https://cdn.chotot.com", HTTP: &http.Client{}}
		if _, err := c.Ad(context.Background(), 119482736); !errors.Is(err, chotot.ErrUnavailable) {
			t.Fatalf("got %v", err)
		}
	})
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := range 24 {
		for x := range 32 {
			m.Set(x, y, color.RGBA{uint8(x * 8), uint8(y * 10), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestImage(t *testing.T) {
	t.Parallel()
	photo := jpegBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.jpg":
			_, _ = w.Write(photo)
		case "/moved.jpg":
			http.Redirect(w, r, "/a.jpg", http.StatusMovedPermanently)
		case "/page.jpg":
			_, _ = w.Write([]byte("<html><body>not found</body></html>"))
		case "/big.jpg":
			_, _ = w.Write(append(photo, make([]byte, chotot.MaxImageBytes)...))
		case "/down.jpg":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/blocked.jpg":
			w.WriteHeader(http.StatusForbidden)
		case "/busy.jpg":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := chotot.Client{GatewayURL: "http://unused.invalid", CDNURL: srv.URL, HTTP: srv.Client()}
	b, ct, err := c.Image(context.Background(), srv.URL+"/a.jpg")
	if err != nil || ct != "image/jpeg" || !bytes.Equal(b, photo) {
		t.Fatalf("%d bytes, %q, %v", len(b), ct, err)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	cases := []struct {
		name, url string
		want      error
	}{
		{"another host", "http://example.invalid/a.jpg", chotot.ErrOffCDN},
		{"another scheme", "https://" + host + "/a.jpg", chotot.ErrOffCDN},
		{"user info", "http://x@" + host + "/a.jpg", chotot.ErrBadImage},
		{"not a URL", "::", chotot.ErrBadImage},
		{"redirect", srv.URL + "/moved.jpg", chotot.ErrBadImage},
		{"an HTML page", srv.URL + "/page.jpg", chotot.ErrBadImage},
		{"over 15 MB", srv.URL + "/big.jpg", chotot.ErrBadImage},
		{"gone", srv.URL + "/gone.jpg", chotot.ErrBadImage},
		{"CDN down", srv.URL + "/down.jpg", chotot.ErrUnavailable},
		{"the server blocked", srv.URL + "/blocked.jpg", chotot.ErrUnavailable},
		{"rate limited", srv.URL + "/busy.jpg", chotot.ErrUnavailable},
	}
	if !errors.Is(chotot.ErrOffCDN, chotot.ErrBadImage) {
		t.Fatal("a photo off the CDN is a bad image too")
	}
	for _, tc := range cases {
		if _, _, err := c.Image(context.Background(), tc.url); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
}

const oid = "8a3f2c91d07b4e5fa1b2c3d4e5f60718"

// routes serves each path's body, 404 for any other path.
func routes(t *testing.T, bodies map[string]string) chotot.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := bodies[r.URL.RequestURI()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if b == "500" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(b))
	}))
	t.Cleanup(srv.Close)
	return chotot.Client{GatewayURL: srv.URL, CDNURL: "https://cdn.chotot.com", HTTP: srv.Client()}
}

// The profile keeps its name, avatar and bio; the phone, address and
// identity numbers beside them are never decoded.
func TestProfile(t *testing.T) {
	t.Parallel()
	c := routes(t, map[string]string{
		"/v1/public/profile/" + oid: `{"account_oid":"` + strings.ToUpper(oid) + `","account_id":23600386,"full_name":" Linh Nguyễn ",
			"avatar":"https://cdn.chotot.com/uac2/23600386","description":"Mã SN-7K3Q9M","phone":"0909798718",
			"address":"1 Đường Mẫu","id_number":"079000000000","email_verified":true}`,
		"/v1/public/profile/00000000000000000000000000000001": `{"account_oid":"other","account_id":1}`,
		"/v1/public/profile/00000000000000000000000000000002": "500",
	})
	p, err := c.Profile(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if p.OID != oid || p.AccountID != 23600386 || p.Name != "Linh Nguyễn" || p.AvatarURL != "https://cdn.chotot.com/uac2/23600386" ||
		p.Description != "Mã SN-7K3Q9M" {
		t.Fatalf("%+v", p)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"0909798718", "Đường Mẫu", "079000000000"} {
		if bytes.Contains(b, []byte(s)) {
			t.Errorf("the profile carries %q", s)
		}
	}
	for id, want := range map[string]error{
		"00000000000000000000000000000000": chotot.ErrNotFound,
		"00000000000000000000000000000001": chotot.ErrFormat,
		"00000000000000000000000000000002": chotot.ErrUnavailable,
	} {
		if _, err = c.Profile(context.Background(), id); !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", id, err, want)
		}
	}
}

// Ads pages through the seller's listings and keeps only the active ones
// of the profile asked for.
func TestAds(t *testing.T) {
	t.Parallel()
	ad := func(id int, owner, status string) string {
		return fmt.Sprintf(`{"list_id":%d,"account_oid":%q,"subject":"Tin %d","price":%d,"category":5010,`+
			`"category_name":"Điện thoại","image":"https://cdn.chotot.com/%d.jpg","status":%q,"phone":"0909"}`, id, owner, id, id*1000, id, status)
	}
	first := make([]string, 50)
	for i := range first {
		first[i] = ad(100+i, oid, "active")
	}
	first[1] = ad(101, "someone-else", "active")
	first[2] = ad(102, oid, "hidden")
	c := routes(t, map[string]string{
		"/v1/public/ad-listing?account_id=7&limit=50&o=0":  `{"ads":[` + strings.Join(first, ",") + `],"total":52}`,
		"/v1/public/ad-listing?account_id=7&limit=50&o=50": `{"ads":[` + ad(150, oid, "active") + `,` + ad(151, oid, "active") + `],"total":52}`,
		"/v1/public/ad-listing?account_id=8&limit=50&o=0":  `{"ads":[]}`,
		"/v1/public/ad-listing?account_id=9&limit=50&o=0":  "500",
	})
	got, err := c.Ads(context.Background(), 7, oid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 || got[0].ListID != 100 || got[1].ListID != 103 || got[49].ListID != 151 ||
		got[0].Price != 100_000 || got[0].Category != 5010 || got[0].Thumb != "https://cdn.chotot.com/100.jpg" {
		t.Fatalf("%d ads: %+v", len(got), got[:2])
	}
	if _, err = c.Ads(context.Background(), 8, oid); !errors.Is(err, chotot.ErrFormat) {
		t.Errorf("no total: %v", err)
	}
	if _, err = c.Ads(context.Background(), 9, oid); !errors.Is(err, chotot.ErrUnavailable) {
		t.Errorf("down: %v", err)
	}
	if !chotot.RealEstate(1020) || chotot.RealEstate(5010) {
		t.Error("real estate is 1000 to 1999")
	}
}
