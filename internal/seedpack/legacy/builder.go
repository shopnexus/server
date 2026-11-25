package legacy

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/pflag"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/seedpack"
	"github.com/shopnexus/server/internal/seedpack/corpus"
)

// TextModel is the old database's dense vector: BGE-M3 at the version the
// server embeds with.
const TextModel = "bge-m3@5617a9f61b028005a4858fdac845db406aefb181"

// ref is the moment the pack is set at: the newest listing lands a day
// before it and nothing happens after it.
var ref = time.Date(2026, 11, 19, 2, 0, 0, 0, time.UTC)

// firstPhone is the first legacy number; the demo pack starts at 84900199001.
const firstPhone = 84900100000

// Builder is `seedpack build legacy`.
type Builder struct {
	db, objects, avatars string
	sample, workers      int
	soldRate             float64
}

func (*Builder) Name() string  { return "legacy" }
func (*Builder) Short() string { return "Build a pack from the old ShopNexus dev database" }
func (*Builder) Example() string {
	return `  # On a copy of the old database: psql -f internal/seedpack/legacy/export.sql
  seedpack build legacy --db postgres://legacy:legacy@127.0.0.1:55499/legacy \
    --objects ~/shopnexus/tmp/legacy/objects --avatars ~/shopnexus/tmp/seedpack-assets/avatars \
    --out ~/shopnexus/tmp/packs/vn-catalog
  seedpack build legacy ... --sample 10000 --name vn-catalog-10k --out ~/shopnexus/tmp/packs/vn-catalog-10k`
}
func (*Builder) Pack() (name, version string) { return "vn-catalog", "2026.11.19" }

func (b *Builder) Flags(fs *pflag.FlagSet) {
	fs.StringVar(&b.db, "db", "", "a Postgres holding the export schema of export.sql (required)")
	fs.StringVar(&b.objects, "objects", "", "the old object store's folder, holding catalog/... (required)")
	fs.StringVar(&b.avatars, "avatars", "", "a folder of avatar squares with index.json (optional)")
	fs.IntVar(&b.sample, "sample", 0, "keep this many listings, spread over the categories (0 keeps all)")
	fs.IntVar(&b.workers, "workers", 8, "photos processed at once")
	fs.Float64Var(&b.soldRate, "sold", 0.22, "the share of active listings written as sold, with an order and reviews")
}

// old is one listing of the export.
type old struct {
	id, account                  int64
	category, title, description string
	specs                        map[string]string
	condition, status            string
	createdAt                    time.Time
	price                        int64
	photos                       []string
	vector                       []float32
}

// item is a listing as the pack writes it, with what the builder knows of it.
type item struct {
	l      seedpack.Listing
	vector []float32
	sold   bool
}

// person is an account of the pack while it is built.
type person struct {
	a       seedpack.Account
	gender  string
	sells   []*item
	commune Commune
}

func (b *Builder) Build(ctx context.Context, w *seedpack.Writer, p seedpack.Progress) error {
	if b.db == "" || b.objects == "" {
		return errors.New("--db and --objects are required")
	}
	cp, err := corpus.Load()
	if err != nil {
		return err
	}
	communes, err := Communes()
	if err != nil {
		return err
	}
	picker := NewPicker(communes)
	olds, err := b.read(ctx, p)
	if err != nil {
		return err
	}
	if b.sample > 0 {
		olds = sample(olds, b.sample)
	}
	shift := ref.Add(-24 * time.Hour).Sub(newest(olds))

	// Photos first, in parallel: the listing lines need their SHA-256s.
	shas, err := b.photos(w, p, olds)
	if err != nil {
		return err
	}

	// People: an old seller's listings go to several people of at most 18
	// on sale each, so the cap of 20 holds with room to post.
	var people []*person
	items := make([]*item, 0, len(olds))
	byAccount := map[int64][]*old{}
	var order []int64
	for i := range olds {
		o := &olds[i]
		if byAccount[o.account] == nil {
			order = append(order, o.account)
		}
		byAccount[o.account] = append(byAccount[o.account], o)
	}
	newPerson := func(seed string) *person {
		n := len(people)
		g := []string{"f", "m"}[int(u(seed, "gender")*2)]
		c := picker.Pick(u(seed, "province"), u(seed, "ward"), u(seed, "commune"))
		pr := &person{gender: g, commune: c, a: seedpack.Account{
			Key: fmt.Sprintf("a-%06d", n), Phone: fmt.Sprintf("+%d", firstPhone+n), Commune: c.Code,
			Name: cp.Name(g, [5]float64{u(seed, "n0"), u(seed, "n1"), u(seed, "n2"), u(seed, "n3"), u(seed, "n4")}),
		}}
		people = append(people, pr)
		return pr
	}
	placeholder := &person{a: seedpack.Account{Key: "check"}, commune: Commune{Code: "check"}}
	invalid := 0
	for _, acc := range order {
		list := byAccount[acc]
		slices.SortFunc(list, func(x, y *old) int { return x.createdAt.Compare(y.createdAt) })
		var cur *person
		onSale, quota := 0, 0
		for _, o := range list {
			key := fmt.Sprintf("l-%d", o.id)
			// A listing whose photos processing all refused, or that breaks
			// another rule, stays out.
			if err = seedpack.CheckListing(newItem(o, key, placeholder, shas, shift).l); err != nil {
				invalid++
				if invalid <= 5 {
					p.Warnf("%v", err)
				}
				continue
			}
			sold := o.status == seedpack.StatusActive && u(key, "sold") < b.soldRate && ref.Sub(o.createdAt.Add(shift)) > 10*24*time.Hour
			if cur == nil || (!sold && onSale >= quota) {
				cur = newPerson(fmt.Sprintf("p-%d-%d", acc, len(people)))
				onSale, quota = 0, shopSize(u(cur.a.Key, "size"))
			}
			if !sold {
				onSale++
			}
			it := newItem(o, key, cur, shas, shift)
			it.sold = sold
			cur.sells = append(cur.sells, it)
			items = append(items, it)
		}
	}
	if invalid > 0 {
		p.Warnf("%d listings left out for breaking the server's rules", invalid)
	}
	// Buyers who never sold, a third as many as the sellers.
	sellers := len(people)
	for i := range sellers / 3 {
		newPerson(fmt.Sprintf("buyer-%d", i))
	}

	avatars, err := b.loadAvatars(w)
	if err != nil {
		return err
	}
	for _, pr := range people {
		fillProfile(pr, cp, avatars)
	}

	// Lines: people, then listings and sales in one order, which the text
	// vectors follow.
	bar := p.Stage("people", int64(len(people)))
	for _, pr := range people {
		if err = w.Account(pr.a); err != nil {
			return err
		}
		bar.Add(1)
	}
	bar.Done()
	var listings, sales []*item
	for _, it := range items {
		if it.sold {
			sales = append(sales, it)
		} else {
			listings = append(listings, it)
		}
	}
	bar = p.Stage("listings", int64(len(items)))
	for _, it := range listings {
		if err = w.Listing(it.l); err != nil {
			return err
		}
		bar.Add(1)
	}
	byKey := map[string]*person{}
	for _, pr := range people {
		byKey[pr.a.Key] = pr
	}
	for _, it := range sales {
		if err = w.Sale(newSale(it, people, byKey, cp)); err != nil {
			return err
		}
		bar.Add(1)
	}
	bar.Done()
	for _, f := range follows(people[:sellers], people) {
		if err = w.Follow(f); err != nil {
			return err
		}
	}

	m, err := seedpack.CreateMatrix(filepath.Join(w.Dir(), filepath.FromSlash(seedpack.TextVectors)), len(listings)+len(sales), seedpack.TextDims)
	if err != nil {
		return err
	}
	missing := 0
	for i, it := range slices.Concat(listings, sales) {
		if it.vector == nil {
			missing++
			continue
		}
		if err = m.SetRow(i, it.vector); err != nil {
			_ = m.Close()
			return err
		}
	}
	if err = m.Close(); err != nil {
		return err
	}
	if missing > 0 {
		p.Warnf("%d listings have no old text vector; a load leaves them to ListingEmbed", missing)
	}
	w.Manifest.License = "internal-only"
	w.Manifest.Source = "ShopNexus legacy dev database (branch escrow), originally Tiki product pages"
	w.Manifest.Vectors.Text = &seedpack.VectorSet{Model: TextModel, Origin: seedpack.OriginLegacy}
	w.Readme = readme
	return nil
}

// read loads the export's listings that map to a category, with their old
// vectors.
func (b *Builder) read(ctx context.Context, p seedpack.Progress) ([]old, error) {
	db, err := pgx.Connect(ctx, b.db)
	if err != nil {
		return nil, fmt.Errorf("connect to the legacy database: %w", err)
	}
	defer func() { _ = db.Close(ctx) }()
	var total int64
	if err = db.QueryRow(ctx, `SELECT count(*) FROM export.listing`).Scan(&total); err != nil {
		return nil, fmt.Errorf("count the export: %w", err)
	}
	bar := p.Stage("read", total)
	rows, err := db.Query(ctx, `
SELECT l.id, l.account_id, c.name, l.name, l.description, l.specifications, l.condition, l.status, l.created_at,
       l.price, l.photos, v.dense::text
FROM export.listing l
JOIN export.category c ON c.id = l.category_id
LEFT JOIN export.text_vector v ON v.listing_id = l.id
ORDER BY l.id`)
	if err != nil {
		return nil, fmt.Errorf("read the export: %w", err)
	}
	defer rows.Close()
	var out []old
	dropped := 0
	for rows.Next() {
		var o old
		var specs []byte
		var vector *string
		if err = rows.Scan(&o.id, &o.account, &o.category, &o.title, &o.description, &specs, &o.condition, &o.status,
			&o.createdAt, &o.price, &o.photos, &vector); err != nil {
			return nil, fmt.Errorf("read a listing: %w", err)
		}
		bar.Add(1)
		if _, ok := Category(o.category, o.title); !ok {
			dropped++
			continue
		}
		_ = json.Unmarshal(specs, &o.specs)
		if vector != nil {
			o.vector = parseVector(*vector)
		}
		if len(o.photos) > catalogdomain.MaxPhotos {
			o.photos = o.photos[:catalogdomain.MaxPhotos]
		}
		out = append(out, o)
	}
	bar.Done()
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the export: %w", err)
	}
	p.Warnf("%d of %d listings dropped: groceries, consumables and goods nobody resells", dropped, total)
	return out, nil
}

// photos processes every photo as the server does and adds it to the pack,
// returning the SHA-256 of each old object key. A photo processing refuses
// is left out of its listing.
func (b *Builder) photos(w *seedpack.Writer, p seedpack.Progress, olds []old) (map[string]string, error) {
	var keys []string
	seen := map[string]bool{}
	for i := range olds {
		for _, k := range olds[i].photos {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	bar := p.Stage("photos", int64(len(keys)))
	defer bar.Done()
	shas := make(map[string]string, len(keys))
	var mu sync.Mutex
	work := make(chan string)
	var wg sync.WaitGroup
	for range max(b.workers, 1) {
		wg.Go(func() {
			for k := range work {
				raw, err := os.ReadFile(filepath.Join(b.objects, filepath.FromSlash(k)))
				var photo, thumb []byte
				if err == nil {
					photo, thumb, err = seedpack.ProcessPhoto(raw)
				}
				var sha string
				if err == nil {
					sha, err = w.AddPhoto(photo, thumb)
				}
				mu.Lock()
				if err != nil {
					p.Warnf("photo %s: %v", k, err)
				} else {
					shas[k] = sha
				}
				mu.Unlock()
				bar.Add(1)
			}
		})
	}
	for _, k := range keys {
		work <- k
	}
	close(work)
	wg.Wait()
	return shas, nil
}

// item maps an old listing onto the pack's rules.
func newItem(o *old, key string, seller *person, shas map[string]string, shift time.Duration) *item {
	leaf, _ := Category(o.category, o.title)
	title := CleanTitle(o.title, catalogdomain.MaxTitle)
	desc := strings.ReplaceAll(o.description, o.title, title)
	if utf8.RuneCountInString(desc) > catalogdomain.MaxDescription {
		desc = string([]rune(desc)[:catalogdomain.MaxDescription])
	}
	condition := map[string]string{"new": "new", "used": "used", "damaged": "used"}[o.condition]
	if condition == "used" && o.condition != "damaged" && u(key, "like_new") < 0.4 {
		condition = "like_new"
	}
	var photos []string
	for _, k := range o.photos {
		if sha, ok := shas[k]; ok && !slices.Contains(photos, sha) {
			photos = append(photos, sha)
		}
	}
	commune := seller.commune.Code
	status := ""
	if o.status == seedpack.StatusHidden {
		status = seedpack.StatusHidden
	}
	return &item{vector: o.vector, l: seedpack.Listing{
		Key: key, Seller: seller.a.Key, Title: title, Price: roundPrice(o.price), Description: strings.TrimSpace(desc),
		Category: leaf, Condition: condition, AllowOffers: u(key, "offers") < 0.7, Commune: commune,
		Specs: specs(o.specs), Photos: photos, CreatedAt: o.createdAt.Add(shift).UTC().Truncate(time.Second), Status: status,
	}}
}

// sale turns a sold item into its order and reviews: a buyer of the same
// province more often than not, delivered within days, reviewed both ways.
func newSale(it *item, people []*person, byKey map[string]*person, cp *corpus.Corpus) seedpack.Sale {
	l := it.l
	seller := byKey[l.Seller]
	key := l.Key
	var buyer *person
	for try := range 20 {
		c := people[int(u(key, fmt.Sprint("buyer", try))*float64(len(people)))]
		if c == seller || (try < 12 && c.commune.Province != seller.commune.Province) {
			continue
		}
		buyer = c
		break
	}
	if buyer == nil {
		buyer = people[(slices.Index(people, seller)+1)%len(people)]
	}
	price := l.Price
	if l.AllowOffers && u(key, "haggle") < 0.5 {
		price = max(roundPrice(int64(float64(l.Price)*(0.8+0.2*u(key, "deal")))), catalogdomain.MinPrice)
		price = min(price, l.Price)
	}
	latest := ref.Add(-72 * time.Hour)
	ordered := l.CreatedAt.Add(time.Duration((1 + 40*u(key, "wait")) * float64(24*time.Hour)))
	if ordered.After(latest.Add(-6 * 24 * time.Hour)) {
		ordered = l.CreatedAt.Add(time.Duration(u(key, "wait") * float64(latest.Add(-6*24*time.Hour).Sub(l.CreatedAt))))
	}
	ordered = ordered.Truncate(time.Second)
	shipped := ordered.Add(time.Duration((6 + 30*u(key, "ship")) * float64(time.Hour))).Truncate(time.Second)
	delivered := shipped.Add(time.Duration((30 + 70*u(key, "road")) * float64(time.Hour))).Truncate(time.Second)
	s := seedpack.Sale{Listing: l, Buyer: buyer.a.Key, Price: price, OrderedAt: ordered, ShippedAt: &shipped, DeliveredAt: delivered}
	root, _, _ := strings.Cut(l.Category, ".")
	for _, role := range []string{seedpack.RoleSeller, seedpack.RoleBuyer} {
		rate := map[string]float64{seedpack.RoleSeller: 0.78, seedpack.RoleBuyer: 0.6}[role]
		if u(key, role+"reviewed") >= rate {
			continue
		}
		stars := corpus.Stars(role, u(key, role+"stars"))
		at := delivered.Add(time.Duration((2 + 70*u(key, role+"at")) * float64(time.Hour))).Truncate(time.Second)
		r := seedpack.Review{
			Role: role, Stars: stars, At: at,
			Text: cp.ReviewText(role, stars, root, [3]float64{u(key, role+"t0"), u(key, role+"t1"), u(key, role+"t2")}),
		}
		if at.Before(ref) {
			s.Reviews = append(s.Reviews, r)
		}
	}
	// A reply comes once its review shows: after the other side's review,
	// or after the blind week from the close, an hour after delivery.
	shown := delivered.Add(time.Hour + 8*24*time.Hour)
	if len(s.Reviews) == 2 {
		shown = maxTime(s.Reviews[0].At, s.Reviews[1].At)
	}
	for i := range s.Reviews {
		r := &s.Reviews[i]
		text, ok := cp.ReplyText(r.Role, r.Stars, [2]float64{u(key, r.Role+"r0"), u(key, r.Role+"r1")})
		replied := maxTime(shown, r.At).Add(time.Duration((1 + 40*u(key, r.Role+"rat")) * float64(time.Hour))).Truncate(time.Second)
		if ok && replied.Before(ref) {
			r.Reply = &seedpack.Reply{Text: text, At: replied}
		}
	}
	return s
}

// profile fills a person's bio, avatar, dates and address.
func fillProfile(pr *person, cp *corpus.Corpus, avatars []avatar) {
	key := pr.a.Key
	first := ref.Add(-time.Duration(30+300*u(key, "buyerage")) * 24 * time.Hour)
	for _, it := range pr.sells {
		first = minTime(first, it.l.CreatedAt)
	}
	joined := first.Add(-time.Duration(u(key, "joined")*u(key, "joined2")*600) * 24 * time.Hour).Truncate(time.Second)
	pr.a.JoinedAt = joined
	last := ref.Add(-time.Duration(math.Min(-math.Log(1-u(key, "active"))*4, 120) * float64(24*time.Hour)))
	if len(pr.sells) == 0 {
		last = ref.Add(-time.Duration(u(key, "active")*90) * 24 * time.Hour)
	}
	last = maxTime(last, joined).Truncate(time.Second)
	pr.a.LastActiveAt = &last
	pr.a.Bio = cp.BioText(persona(pr, u(key, "persona")), [2]float64{u(key, "bio0"), u(key, "bio1")})
	rate := 0.55
	if len(pr.sells) == 0 {
		rate = 0.35
	}
	if len(avatars) > 0 && u(key, "hasavatar") < rate {
		pr.a.Avatar = pickAvatar(avatars, pr.gender, u(key, "avatar"), u(key, "avatarkind"))
	}
	kind := "home"
	if k := u(key, "addrkind"); k > 0.95 {
		kind = "other"
	} else if k > 0.75 {
		kind = "work"
	}
	pr.a.Address = &seedpack.Address{
		Street:  fmt.Sprintf("%d %s", 1+int(u(key, "no")*299), streets[int(u(key, "street")*float64(len(streets)))]),
		Commune: pr.commune.Code, Kind: kind,
	}
}

// follows has each person follow a few sellers, the busier ones more.
func follows(sellers, people []*person) []seedpack.Follow {
	weights := make([]float64, 0, len(sellers))
	total := 0.0
	for _, s := range sellers {
		w := math.Sqrt(float64(len(s.sells)))
		weights = append(weights, w)
		total += w
	}
	var out []seedpack.Follow
	for _, pr := range people {
		key := pr.a.Key
		n := int(-math.Log(1-u(key, "follows")) * 3)
		seen := map[string]bool{}
		for i := range min(n, 40) {
			at := u(key, fmt.Sprint("f", i)) * total
			var s *person
			for j, w := range weights {
				if at -= w; at < 0 {
					s = sellers[j]
					break
				}
			}
			if s == nil || s == pr || seen[s.a.Key] {
				continue
			}
			seen[s.a.Key] = true
			from := maxTime(pr.a.JoinedAt, s.a.JoinedAt)
			out = append(out, seedpack.Follow{
				Follower: key, Followee: s.a.Key,
				At: from.Add(time.Duration(u(key, s.a.Key) * float64(ref.Sub(from)))).Truncate(time.Second),
			})
		}
	}
	return out
}

// shopSize draws how many listings a person keeps on sale: most have a
// few, some up to 18.
func shopSize(x float64) int {
	switch {
	case x < 0.22:
		return 1
	case x < 0.38:
		return 2
	case x < 0.50:
		return 3
	case x < 0.70:
		return 4 + int((x-0.50)/0.2*3)
	case x < 0.88:
		return 7 + int((x-0.70)/0.18*6)
	default:
		return 13 + int((x-0.88)/0.12*6)
	}
}

// roundPrice rounds as people price: to 1,000 under 100,000, else to 5,000,
// within the server's bounds.
func roundPrice(p int64) int64 {
	step := int64(5000)
	if p < 100_000 {
		step = 1000
	}
	p = (p + step/2) / step * step
	return min(max(p, catalogdomain.MinPrice), catalogdomain.MaxPrice)
}

// specs keeps the old specs the server takes, condition left out: it has
// its own field.
func specs(m map[string]string) []seedpack.Spec {
	labels := slices.Sorted(func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	})
	out := []seedpack.Spec{}
	for _, k := range labels {
		label, value := cut(strings.TrimSpace(k), catalogdomain.MaxSpecText), cut(strings.TrimSpace(m[k]), catalogdomain.MaxSpecText)
		if label == "" || value == "" || strings.EqualFold(label, "Tình trạng") {
			continue
		}
		out = append(out, seedpack.Spec{Label: label, Value: value})
		if len(out) == catalogdomain.MaxSpecs {
			break
		}
	}
	return out
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}

// persona is the kind of bio that fits what a person sells.
func persona(pr *person, x float64) string {
	roots := map[string]int{}
	for _, it := range pr.sells {
		root, _, _ := strings.Cut(it.l.Category, ".")
		roots[root]++
	}
	top, n := "", 0
	for r, c := range roots {
		if c > n || (c == n && r < top) {
			top, n = r, c
		}
	}
	switch {
	case len(pr.sells) == 0:
		return []string{"student", "office", "general"}[int(x*3)]
	case len(pr.sells) >= 12 && x < 0.4:
		return "reseller"
	case top == "baby":
		return "mom"
	case top == "phones" || top == "laptop" || top == "camera":
		return "tech"
	case top == "furniture" || top == "home":
		return []string{"mover", "general"}[int(x*2)]
	case top == "hobby" || top == "books":
		return "collector"
	}
	return []string{"general", "office", "student"}[int(x*3)]
}

// sample keeps n listings, each category in proportion, by a hash of the id.
func sample(olds []old, n int) []old {
	if n >= len(olds) {
		return olds
	}
	slices.SortFunc(olds, func(x, y old) int {
		return int(math.Copysign(1, u(strconv.FormatInt(x.id, 10), "sample")-u(strconv.FormatInt(y.id, 10), "sample")))
	})
	return olds[:n]
}

func newest(olds []old) time.Time {
	var t time.Time
	for i := range olds {
		t = maxTime(t, olds[i].createdAt)
	}
	return t
}

// u is a number in [0, 1) fixed by key and salt, so a rebuild writes the
// same pack.
func u(key, salt string) float64 {
	h := sha256.Sum256([]byte(key + "\x00" + salt))
	return float64(binary.BigEndian.Uint64(h[:8])>>11) / (1 << 53)
}

func parseVector(s string) []float32 {
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")
	if len(parts) != seedpack.TextDims {
		return nil
	}
	v := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(p, 32)
		if err != nil {
			return nil
		}
		v[i] = float32(f)
	}
	return v
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

var streets = []string{
	"Lê Lợi", "Nguyễn Huệ", "Trần Hưng Đạo", "Lý Thường Kiệt", "Hai Bà Trưng", "Nguyễn Trãi", "Lê Văn Sỹ",
	"Cách Mạng Tháng Tám", "Điện Biên Phủ", "Võ Văn Tần", "Nguyễn Thị Minh Khai", "Phan Đình Phùng", "Quang Trung",
	"Hoàng Văn Thụ", "Cộng Hòa", "Xô Viết Nghệ Tĩnh", "Phan Xích Long", "Nguyễn Văn Linh", "Lạc Long Quân",
	"Âu Cơ", "Tô Hiến Thành", "Bạch Đằng", "Hùng Vương", "Trường Chinh", "Lê Duẩn", "Kim Mã", "Cầu Giấy",
	"Xuân Thủy", "Láng Hạ", "Tôn Đức Thắng", "Nguyễn Chí Thanh", "Giải Phóng", "Hoàng Quốc Việt", "Ngô Quyền",
	"Trần Phú", "Nguyễn Văn Cừ", "Lê Hồng Phong", "Phạm Văn Đồng", "Kha Vạn Cân", "Võ Thị Sáu",
}

//go:embed README.pack.md
var readme string
