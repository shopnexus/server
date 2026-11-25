package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// The listings the Chợ Tốt stub tells apart, by list ID.
const (
	iphoneListing = 119482736 // the seller's iPhone, 3 photos
	flatListing   = 119400001 // a flat for rent: no item
	downListing   = 119400002 // the gateway answers 500
	brokenListing = 119400003 // every photo is an HTML page
)

// stubChotot is Chợ Tốt's gateway and CDN on one server. Each profile a
// test makes lists two listings, numbered from its account: an iPhone
// (account×1000+1) and a flat for rent (account×1000+2).
type stubChotot struct {
	srv      *httptest.Server
	mu       sync.Mutex
	profiles map[string]*stubProfile
	next     int64
}

// stubProfile is a profile's bio; down makes its profile, adsDown its
// listings, answer 500.
type stubProfile struct {
	account       int64
	bio           string
	down, adsDown bool
}

// newProfile makes a profile with bio and returns its ID.
func (c *stubChotot) newProfile(bio string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	oid := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	c.profiles[oid] = &stubProfile{account: 1_000_000 + c.next, bio: bio}
	return oid
}

// change edits profile oid.
func (c *stubChotot) change(oid string, edit func(*stubProfile)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	edit(c.profiles[oid])
}

// listIDs are the iPhone's and the flat's list IDs of profile oid.
func (c *stubChotot) listIDs(oid string) (iphone, flat int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.profiles[oid].account
	return a*1000 + 1, a*1000 + 2
}

// owner is the profile whose account lists listID, "" for none.
func (c *stubChotot) owner(listID int64) (string, *stubProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for oid, p := range c.profiles {
		if p.account == listID/1000 {
			cp := *p
			return oid, &cp
		}
	}
	return "", nil
}

// profileAnswer serves /v1/public/profile/<oid>, with the fields the
// server must not keep beside the ones it reads.
func (c *stubChotot) profileAnswer(w http.ResponseWriter, oid string) {
	c.mu.Lock()
	p, ok := c.profiles[oid]
	var cp stubProfile
	if ok {
		cp = *p
	}
	c.mu.Unlock()
	switch {
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	case cp.down:
		w.WriteHeader(http.StatusInternalServerError)
	default:
		writeStubJSON(w, map[string]any{
			"account_oid": oid, "account_id": cp.account, "full_name": "Shop Mẫu", "avatar": "", "description": cp.bio,
			"phone": "0909000000", "address": "1 Đường Mẫu", "id_number": "079000000000",
		})
	}
}

// adsAnswer serves /v1/public/ad-listing?account_id=, the newest first.
func (c *stubChotot) adsAnswer(w http.ResponseWriter, r *http.Request) {
	var account int64
	_, _ = fmt.Sscan(r.URL.Query().Get("account_id"), &account)
	oid, p := c.owner(account * 1000)
	if p == nil {
		writeStubJSON(w, map[string]any{"ads": []any{}, "total": 0})
		return
	}
	if p.adsDown {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	ad := func(id int64, subject string, price int64, category int) map[string]any {
		return map[string]any{
			"list_id": id, "account_oid": oid, "subject": subject, "price": price, "category": category,
			"category_name": "Điện thoại", "image": c.srv.URL + "/photo/0.jpg", "status": "active",
		}
	}
	writeStubJSON(w, map[string]any{"ads": []any{
		ad(account*1000+2, "Cho thuê căn hộ 2 phòng ngủ", 8_000_000, 1050),
		ad(account*1000+1, "iPhone 11 64GB trắng pin 81% zin all", 4_500_000, 5010),
	}, "total": 2})
}

func newChotot() *stubChotot {
	c := &stubChotot{profiles: map[string]*stubProfile{}}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/public/profile/"):
			c.profileAnswer(w, strings.TrimPrefix(r.URL.Path, "/v1/public/profile/"))
		case r.URL.Path == "/v1/public/ad-listing":
			c.adsAnswer(w, r)
		case strings.HasPrefix(r.URL.Path, "/v1/public/ad-listing/"):
			var id int64
			if _, err := fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/v1/public/ad-listing/"), &id); err != nil {
				http.NotFound(w, r)
				return
			}
			subject, body := "iPhone 11 64GB trắng pin 81% zin all", "Máy dùng 3 năm, zin all. Có thương lượng. Liên hệ 0909 123 456."
			photo, owner, category := "photo", "", 5010
			if oid, p := c.owner(id); p != nil {
				owner = oid
				if id%1000 == 2 {
					subject, body, category = "Cho thuê căn hộ 2 phòng ngủ", "Căn hộ Quận 7, có nội thất.", 1050
				}
				id = -id // served below as the iPhone or the flat
			}
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
				if id < 0 {
					id = -id
					break
				}
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
					"list_id": id, "account_oid": owner, "category": category,
					"subject": subject, "body": body, "price": 4_500_000, "category_name": "Điện thoại",
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
