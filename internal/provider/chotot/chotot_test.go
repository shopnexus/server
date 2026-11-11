package chotot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/provider/chotot"
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
	if ad.ListID != 119482736 || ad.Subject != "iPhone 11 64GB trắng pin 81% zin all" || ad.Price != 4_500_000 ||
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
		{"too large", http.StatusOK, `{"ad":{"body":"` + strings.Repeat("a", 1<<20) + `"}}`, chotot.ErrFormat},
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
