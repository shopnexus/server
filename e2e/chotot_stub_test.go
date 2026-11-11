package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// The listings the Chợ Tốt stub tells apart, by list ID.
const (
	iphoneListing = 119482736 // the seller's iPhone, 3 photos
	flatListing   = 119400001 // a flat for rent: no item
	downListing   = 119400002 // the gateway answers 500
	brokenListing = 119400003 // every photo is an HTML page
)

// stubChotot is Chợ Tốt's gateway and CDN on one server.
type stubChotot struct{ srv *httptest.Server }

func newChotot() *stubChotot {
	c := &stubChotot{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/public/ad-listing/"):
			var id int64
			if _, err := fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/v1/public/ad-listing/"), &id); err != nil {
				http.NotFound(w, r)
				return
			}
			subject, body := "iPhone 11 64GB trắng pin 81% zin all", "Máy dùng 3 năm, zin all. Có thương lượng. Liên hệ 0909 123 456."
			photo := "photo"
			switch id {
			case iphoneListing:
			case flatListing:
				subject, body = "Cho thuê căn hộ 2 phòng ngủ", "Căn hộ Quận 7, có nội thất."
			case brokenListing:
				photo = "page"
			case downListing:
				w.WriteHeader(http.StatusInternalServerError)
				return
			default:
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"entity not found"}`))
				return
			}
			images := make([]string, 3)
			for i := range images {
				images[i] = fmt.Sprintf("%s/%s/%d.jpg", c.srv.URL, photo, i)
			}
			writeStubJSON(w, map[string]any{
				"ad": map[string]any{
					"list_id": id, "subject": subject, "body": body, "price": 4_500_000, "category_name": "Điện thoại",
					"status": "active", "images": images, "account_name": "Nguyễn Văn Mẫu", "phone": "090000****",
				},
				"parameters": []map[string]string{
					{"id": "mobile_capacity", "label": "Dung lượng", "value": "64 GB"},
					{"id": "address", "label": "Địa chỉ", "value": "1 Đường Mẫu"},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/photo/"):
			var i int
			_, _ = fmt.Sscanf(r.URL.Path, "/photo/%d.jpg", &i)
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(frameJPEG(i))
		case strings.HasPrefix(r.URL.Path, "/page/"):
			_, _ = w.Write([]byte("<html><body>Không tìm thấy</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	return c
}

func (c *stubChotot) url() string { return c.srv.URL }
