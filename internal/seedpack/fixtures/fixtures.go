// Package fixtures builds the demo pack from the app's fixtures: people,
// listings, photos, the reviews of reviews.json as sold items, and follows.
package fixtures

import (
	"context"
	"crypto/sha1" //nolint:gosec // the app names its photos by SHA-1 of the URL
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/shopnexus/server/internal/seedpack"
)

// Builder is `seedpack build fixtures`.
type Builder struct {
	fixtures, photos string
}

func (*Builder) Name() string  { return "fixtures" }
func (*Builder) Short() string { return "Build the demo pack from the app's fixtures" }
func (*Builder) Example() string {
	return `  # The photos left the app's tree; take them from its history.
  mkdir -p tmp/fixture-photos
  git -C ../app archive fedee23c^ core/testing/src/main/resources/photos | tar -x -C tmp/fixture-photos --strip-components=6
  seedpack build fixtures --fixtures ../app/core/data/src/main/resources/fixtures \
    --photos tmp/fixture-photos --out cmd/devseed/data/demo`
}
func (*Builder) Pack() (name, version string) { return "demo", "2026.11.19" }

func (b *Builder) Flags(fs *pflag.FlagSet) {
	fs.StringVar(&b.fixtures, "fixtures", "", "the app's fixtures directory, core/data/src/main/resources/fixtures (required)")
	fs.StringVar(&b.photos, "photos", "", "the fixture photos, named by the first 12 hex of SHA-1 of their URL (required)")
}

// ref is the moment the demo is set at, so a rebuild writes the same bytes.
var ref = time.Date(2026, 11, 19, 2, 0, 0, 0, time.UTC)

// firstPhone is the demo's first number, in the dev range below the legacy
// packs' +849001xxxxx keys.
const firstPhone = 84900199001

var bios = []string{
	"Bán đồ cá nhân dùng kỹ, trả lời tin nhắn buổi tối.",
	"Thanh lý đồ không dùng nữa, giao COD đồng kiểm.",
	"Mê xe đạp, hay lên đời nên bán lại xe cũ.",
	"Đồ công nghệ chính chủ, có hộp và hoá đơn nếu còn.",
}

var streets = map[string]string{
	"26929": "120 Xô Viết Nghệ Tĩnh", "26884": "45 Quang Trung", "27073": "88 Phan Xích Long", "27004": "210 Cộng Hòa",
	"27154": "15 Kỳ Đồng", "27487": "33 Nguyễn Thị Thập", "27190": "70 Ba Tháng Hai", "27343": "12 Trần Hưng Đạo",
	"26740": "9 Lê Lợi",
}

var conditions = map[string]string{"NEW": "new", "LIKE_NEW": "like_new", "USED": "used"}

var conditionWords = map[string]string{
	"new":      "Hàng mới nguyên hộp, mua về chưa dùng tới.",
	"like_new": "Đồ còn rất mới, dùng ít và giữ kỹ, không trầy xước đáng kể.",
	"used":     "Đồ đã qua sử dụng, hoạt động bình thường, có vài vết dùng nhẹ.",
}

type place struct {
	CommuneCode string `json:"communeCode"` //nolint:tagliatelle // the app's fixtures are camelCase
	Commune     string `json:"commune"`
}

type person struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatarUrl"`  //nolint:tagliatelle // the app's fixtures are camelCase
	JoinedYear int    `json:"joinedYear"` //nolint:tagliatelle // the app's fixtures are camelCase
}

type poolItem struct {
	Summary struct {
		ID        string `json:"id"`
		PhotoURL  string `json:"photoUrl"` //nolint:tagliatelle // the app's fixtures are camelCase
		Price     int64  `json:"price"`
		Title     string `json:"title"`
		Condition string `json:"condition"`
	} `json:"summary"`
	Facts struct {
		SellerID     string `json:"sellerId"`     //nolint:tagliatelle // the app's fixtures are camelCase
		CategoryCode string `json:"categoryCode"` //nolint:tagliatelle // the app's fixtures are camelCase
	} `json:"facts"`
	Place place `json:"place"`
}

type detail struct {
	Listing struct {
		ID          string                 `json:"id"`
		Photos      []struct{ URL string } `json:"photos"`
		Specs       []seedpack.Spec        `json:"specs"`
		Description string                 `json:"description"`
		PostedAt    time.Time              `json:"postedAt"` //nolint:tagliatelle // the app's fixtures are camelCase
	} `json:"listing"`
}

type sellerReviews struct {
	Seller  string `json:"seller"`
	Reviews []struct {
		ID     string    `json:"id"`
		Author string    `json:"author"`
		Stars  int       `json:"stars"`
		At     time.Time `json:"at"`
		Text   string    `json:"text"`
		Item   string    `json:"item"`
	} `json:"reviews"`
}

// soldFrom names the listing each reviewed item copies its photo and
// details from.
var soldFrom = map[string]string{
	"AirPods Pro": "p-airpodspro", "iPad Air 4": "p-ipadair", "iPhone 12": "p-iphone12", "Apple Watch S6": "p-watch",
}

// praised are the tags of each reviews.json review, by its ID.
var praised = map[string][]string{"r-1": {"as_described", "quick_reply"}, "r-2": {"well_packed"}, "r-4": {"quick_reply"}}

func (b *Builder) Build(_ context.Context, w *seedpack.Writer, p seedpack.Progress) error {
	if b.fixtures == "" || b.photos == "" {
		return errors.New("--fixtures and --photos are required")
	}
	var (
		pool    []poolItem
		sellers []struct {
			Seller   person   `json:"seller"`
			Place    place    `json:"place"`
			Listings []string `json:"listings"`
		}
		people  []person
		iphone  detail
		reviews sellerReviews
	)
	for name, dst := range map[string]any{
		"pool.json": &pool, "sellers.json": &sellers, "people.json": &people, "listing-p-iphone13.json": &iphone, "reviews.json": &reviews,
	} {
		raw, err := os.ReadFile(filepath.Join(b.fixtures, name))
		if err != nil {
			return fmt.Errorf("read the fixtures: %w", err)
		}
		if err = json.Unmarshal(raw, dst); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	original := func(url string) ([]byte, error) {
		h := sha1.Sum([]byte(url)) //nolint:gosec // a file name, not a secret
		raw, err := os.ReadFile(filepath.Join(b.photos, hex.EncodeToString(h[:])[:12]+".jpg"))
		if err != nil {
			return nil, fmt.Errorf("the photo of %s: %w", url, err)
		}
		return raw, nil
	}
	photo := func(url string) (string, error) {
		raw, err := original(url)
		if err != nil {
			return "", err
		}
		ph, th, err := seedpack.ProcessPhoto(raw)
		if err != nil {
			return "", fmt.Errorf("%s: %w", url, err)
		}
		return w.AddPhoto(ph, th)
	}

	// People: the sellers but Khánh Linh, then three buyers, Linh first.
	type who struct {
		person

		commune, bio string
	}
	var all []who
	var linh person
	for i, s := range sellers {
		if s.Seller.ID == "u-linh" {
			linh = s.Seller
			continue
		}
		all = append(all, who{s.Seller, s.Place.CommuneCode, bios[i%len(bios)]})
	}
	if linh.ID == "" || len(people) < 2 {
		return errors.New("the fixtures lack Khánh Linh or two people")
	}
	for _, buyer := range []person{linh, people[0], people[1]} {
		all = append(all, who{buyer, "26929", ""})
	}
	names := map[string]string{}
	bar := p.Stage("people", int64(len(all)))
	for i, a := range all {
		acc := seedpack.Account{
			Key: a.ID, Name: a.Name, Bio: a.bio, Phone: fmt.Sprintf("+%d", firstPhone+i), Commune: a.commune,
			JoinedAt: time.Date(a.JoinedYear, time.Month(1+i%12), 3+i, 9, 0, 0, 0, time.UTC),
			Address:  &seedpack.Address{Street: streets[a.commune], Commune: a.commune, Kind: "home"},
		}
		if acc.Address.Street == "" {
			acc.Address.Street = "1 Nguyễn Huệ"
		}
		if a.ID == "u-linh" {
			// Linh's COD order goes to her office, so it shows a work address.
			acc.Address = &seedpack.Address{Street: "120 Xô Viết Nghệ Tĩnh", Commune: "26929", Kind: "work"}
		}
		last := ref.Add(-time.Duration(1+3*i) * time.Hour)
		acc.LastActiveAt = &last
		if a.AvatarURL != "" {
			raw, err := original(a.AvatarURL)
			if err != nil {
				return err
			}
			square, err := seedpack.ProcessAvatar(raw)
			if err != nil {
				return fmt.Errorf("avatar of %s: %w", a.ID, err)
			}
			if acc.Avatar, err = w.AddAvatar(square); err != nil {
				return err
			}
		}
		if err := w.Account(acc); err != nil {
			return err
		}
		names[a.Name] = a.ID
		bar.Add(1)
	}
	bar.Done()

	// Listings: every item of the pool; p-iphone13 has its detail page.
	owner := map[string]string{}
	for _, s := range sellers {
		for _, l := range s.Listings {
			owner[l] = s.Seller.ID
		}
	}
	byKey := map[string]seedpack.Listing{}
	bar = p.Stage("listings", int64(len(pool)))
	for i := range pool {
		item := &pool[i]
		s := item.Summary
		seller := owner[s.ID]
		if seller == "" {
			seller = item.Facts.SellerID
		}
		condition := conditions[s.Condition]
		around := strings.TrimPrefix(strings.TrimPrefix(item.Place.Commune, "Phường "), "Xã ")
		l := seedpack.Listing{
			Key: s.ID, Seller: seller, Title: s.Title, Price: s.Price, Category: item.Facts.CategoryCode,
			Condition: condition, AllowOffers: s.ID != "p-r1" && s.ID != "p-nike", Commune: item.Place.CommuneCode,
			Specs:       []seedpack.Spec{},
			Description: conditionWords[condition] + " Xem hàng quanh " + around + ", hoặc gửi COD đồng kiểm toàn quốc.",
			CreatedAt:   ref.Add(-time.Duration(5+i*11) * time.Hour),
		}
		sha, err := photo(s.PhotoURL)
		if err != nil {
			return err
		}
		l.Photos = []string{sha}
		if s.ID == iphone.Listing.ID {
			for _, ph := range iphone.Listing.Photos {
				if sha, err = photo(ph.URL); err != nil {
					p.Warnf("%s: %v", s.ID, err)
					continue
				}
				if sha != l.Photos[0] {
					l.Photos = append(l.Photos, sha)
				}
			}
			l.Specs, l.Description, l.CreatedAt = iphone.Listing.Specs, iphone.Listing.Description, iphone.Listing.PostedAt
		}
		if err = w.Listing(l); err != nil {
			return err
		}
		byKey[l.Key] = l
		bar.Add(1)
	}
	bar.Done()

	// Sold: each item reviews.json reviews, sold by its seller to the
	// author, or to Đức Huy when the author is nobody of the pack.
	bar = p.Stage("sold", int64(len(reviews.Reviews)))
	for i, r := range reviews.Reviews {
		from, ok := byKey[soldFrom[r.Item]]
		if !ok {
			return fmt.Errorf("reviews.json: no listing to copy %q from", r.Item)
		}
		buyer := names[r.Author]
		if buyer == "" || buyer == reviews.Seller {
			buyer = "u-huy"
		}
		delivered := r.At.Add(-26 * time.Hour)
		ordered := delivered.Add(-72 * time.Hour)
		l := from
		l.Key, l.Seller, l.Title = "s-"+r.ID, reviews.Seller, r.Item
		l.CreatedAt, l.Status = ordered.Add(-10*24*time.Hour), seedpack.StatusSold
		sale := seedpack.Sale{
			Listing: l, Buyer: buyer, Price: l.Price / 1000 * 950, OrderedAt: ordered, DeliveredAt: delivered,
			Reviews: []seedpack.Review{{Role: seedpack.RoleSeller, Stars: r.Stars, Text: r.Text, Tags: praised[r.ID], At: r.At}},
		}
		if i%2 == 0 {
			sale.Reviews[0].Reply = &seedpack.Reply{Text: "Cảm ơn bạn nhiều, lần sau ủng hộ mình tiếp nha!", At: r.At.Add(3 * time.Hour)}
			sale.Reviews = append(sale.Reviews, seedpack.Review{
				Role: seedpack.RoleBuyer, Stars: 5, Text: "Chốt nhanh, nhận hàng đúng hẹn.", Tags: []string{"quick_reply", "on_time"},
				At: r.At.Add(time.Hour),
			})
		}
		if err := w.Sale(sale); err != nil {
			return err
		}
		bar.Add(1)
	}
	bar.Done()

	for i, f := range [][2]string{
		{"u-quang", "u-kiet"}, {"u-huy", "u-thuha"}, {"u-maianh", "u-kiet"}, {"u-tri", "u-nam"}, {"u-han", "u-bao"},
	} {
		if err := w.Follow(seedpack.Follow{Follower: f[0], Followee: f[1], At: ref.Add(-time.Duration(2+i*5) * 24 * time.Hour)}); err != nil {
			return err
		}
	}

	w.Manifest.License = "the server repository's license"
	w.Manifest.Source = "ShopNexus app fixtures (core/data/src/main/resources/fixtures); photos from Unsplash"
	w.Readme = readme
	return nil
}

const readme = `# demo

The ShopNexus demo marketplace: the people, listings and photos of the app's
fixtures, four sold items with the reviews of reviews.json, and a few
follows. cmd/devseed loads it and then plays its scenario through the API.

Built with ` + "`seedpack build fixtures`" + `; see its --help. Phones are
+84900199001 to +84900199012. The photos are from Unsplash.
`
