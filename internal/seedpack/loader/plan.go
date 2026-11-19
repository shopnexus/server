// Package loader writes a seed pack into a dev stack through the server's
// entities and repositories, so the server's rules hold for every row. Row
// IDs come from seedpack.ID, so reloads skip and Wipe finds a pack's rows.
package loader

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	accountrepo "github.com/shopnexus/server/internal/account/repository"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/seedpack"
	trustrepo "github.com/shopnexus/server/internal/trust/repository"
)

// Deps is the stack the loader writes to.
type Deps struct {
	Pool  *pgxpool.Pool
	Store *objectstore.Store
	// Ingress and Prefix reach ListingEmbed for the listings left to the
	// server's embedding; nil Ingress sends nothing.
	Ingress *ingress.Client
	Prefix  string
	// TextVersion and ImageVersion are the stack's EMBED_TEXT_VERSION and
	// EMBED_IMAGE_VERSION; empty when that kind of vector is off.
	TextVersion, ImageVersion string
	// Reviews writes a sale's reviews; nil is Reviews, both sides and the
	// replies.
	Reviews ReviewWriter
	Now     func() time.Time
}

// Vector modes of Options.Vectors.
const (
	VectorsPack      = "pack"
	VectorsRecompute = "recompute"
	VectorsNone      = "none"
)

// Stages of Options.Only.
const (
	OnlyAccounts = "accounts"
	OnlyListings = "listings"
	OnlyHistory  = "history"
	OnlyFollows  = "follows"
)

// Options narrow what a load writes.
type Options struct {
	// Vectors is VectorsPack, VectorsRecompute or VectorsNone. Pack writes
	// the pack's vectors of a kind whose model is the stack's and leaves the
	// rest to ListingEmbed; recompute leaves all of them to it.
	Vectors string
	// Only are the stages to run, all when empty; the people a stage needs
	// are written too.
	Only []string
	// Limit keeps the first N listings that pass the filters; 0 keeps all.
	Limit int
	// Categories keep listings under these roots or leaves; Provinces keep
	// those in these province codes.
	Categories, Provinces []string
	// Sellers keeps the listings and sales of the first N sellers; 0 keeps
	// all.
	Sellers int
	// FirstPhotoOnly writes each listing's first photo only.
	FirstPhotoOnly bool
	// HistoryRatio is the share of sales kept, chosen by key; 1 keeps all.
	HistoryRatio float64
	// Workers is how many rows are written at once.
	Workers int
	// EmbedEvery spaces the ListingEmbed refreshes the load sends.
	EmbedEvery time.Duration
}

func (o Options) runs(stage string) bool { return len(o.Only) == 0 || slices.Contains(o.Only, stage) }

// Plan is what a load would write, for the summary before it does.
type Plan struct {
	Pack     seedpack.Manifest `json:"pack"`
	Dir      string            `json:"dir"`
	People   Count             `json:"people"`
	Listings Count             `json:"listings"`
	Photos   int               `json:"photos"`
	Sold     Count             `json:"sold"`
	// Reviews are those the ReviewWriter keeps; Dropped those it cannot
	// store yet.
	Reviews        int      `json:"reviews"`
	ReviewsDropped int      `json:"reviews_dropped"`
	Follows        int      `json:"follows"`
	TextVectors    string   `json:"text_vectors"`
	PhotoVectors   string   `json:"photo_vectors"`
	Skipped        []string `json:"skipped,omitempty"`

	src         *seedpack.Pack
	opts        Options
	accounts    []seedpack.Account
	onSale      []row
	sales       []saleRow
	followLines []seedpack.Follow
	ids         map[string]uuid.UUID // account key to ID
	places      map[string]catalogrepo.Place
	names       map[string]string // category code to display name
	textOn      bool              // write the pack's text vectors
	photoOn     bool
}

// Count is rows the plan writes and, of them, those already there.
type Count struct {
	Total int `json:"total"`
	There int `json:"there"`
}

// row is a listing with its line number, which is its row of text.npy.
type row struct {
	seedpack.Listing

	line int
	id   uuid.UUID
}

// saleRow is a sale with its listing's row.
type saleRow struct {
	row

	sale seedpack.Sale
}

// Loader loads packs into one stack.
type Loader struct {
	d Deps
}

func New(d Deps) *Loader {
	if d.Reviews == nil {
		d.Reviews = Reviews{Reviews: trustrepo.NewReviews(d.Pool)}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Loader{d: d}
}

// Plan reads the pack and the stack and picks what to write: the listings
// and sales that pass the options and whose category is a leaf and commune
// exists, their people, and the follows among them.
func (l *Loader) Plan(ctx context.Context, p *seedpack.Pack, o Options) (*Plan, error) {
	if o.HistoryRatio < 0 || o.HistoryRatio > 1 {
		return nil, fmt.Errorf("the history ratio is %v; want 0 to 1", o.HistoryRatio)
	}
	switch o.Vectors {
	case VectorsPack, VectorsRecompute, VectorsNone:
	default:
		return nil, fmt.Errorf("vectors %q; want pack, recompute or none", o.Vectors)
	}
	for _, s := range o.Only {
		if !slices.Contains([]string{OnlyAccounts, OnlyListings, OnlyHistory, OnlyFollows}, s) {
			return nil, fmt.Errorf("--only %q; want accounts, listings, history or follows", s)
		}
	}
	pl := &Plan{Pack: p.Manifest, Dir: p.Dir, src: p, opts: o, ids: map[string]uuid.UUID{}}
	accounts, err := p.Accounts()
	if err != nil {
		return nil, err
	}
	listings, err := p.Listings()
	if err != nil {
		return nil, err
	}
	var sales []seedpack.Sale
	if o.runs(OnlyHistory) {
		if sales, err = p.History(); err != nil {
			return nil, err
		}
	}
	var follows []seedpack.Follow
	if o.runs(OnlyFollows) {
		if follows, err = p.Follows(); err != nil {
			return nil, err
		}
	}

	// The stack's categories and communes.
	tree, err := catalogrepo.NewCategories(l.d.Pool).Tree(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the categories: %w", err)
	}
	pl.names = map[string]string{}
	leaves := map[string]bool{}
	for _, c := range tree {
		pl.names[c.Code] = c.DisplayName
		if c.ParentCode != "" {
			leaves[c.Code] = true
		}
	}
	codes := map[string]bool{}
	for i := range accounts {
		a := &accounts[i]
		codes[a.Commune] = true
	}
	for i := range listings {
		x := &listings[i]
		codes[x.Commune] = true
	}
	for i := range sales {
		s := &sales[i]
		codes[s.Listing.Commune] = true
	}
	pl.places, err = places(ctx, catalogrepo.NewRegions(l.d.Pool), codes)
	if err != nil {
		return nil, err
	}

	// Listings and sales that pass the filters.
	skipped := map[string]int{}
	keep := func(x seedpack.Listing) bool {
		switch place, ok := pl.places[x.Commune]; {
		case !leaves[x.Category]:
			skipped["listings in a category the stack lacks"]++
			return false
		case !ok:
			skipped["listings in a commune the stack lacks"]++
			return false
		case len(o.Categories) > 0 && !slices.ContainsFunc(o.Categories, func(c string) bool {
			return x.Category == c || strings.HasPrefix(x.Category, c+".")
		}):
			return false
		case len(o.Provinces) > 0 && !slices.Contains(o.Provinces, place.ProvinceCode):
			return false
		}
		return true
	}
	filtered := o.Limit > 0 || o.Sellers > 0 || len(o.Categories) > 0 || len(o.Provinces) > 0 || len(o.Only) > 0
	var sellerOrder []string
	sellerSeen := map[string]bool{}
	addSeller := func(key string) {
		if !sellerSeen[key] {
			sellerSeen[key] = true
			sellerOrder = append(sellerOrder, key)
		}
	}
	var rows []row
	if o.runs(OnlyListings) {
		for i := range listings {
			x := &listings[i]
			if keep(*x) && (o.Limit == 0 || len(rows) < o.Limit) {
				rows = append(rows, row{Listing: *x, line: i})
				addSeller(x.Seller)
			}
		}
	}
	var srows []saleRow
	for j := range sales {
		s := &sales[j]
		if keep(s.Listing) && kept(s.Listing.Key, o.HistoryRatio) && (o.Limit == 0 || sellerSeen[s.Listing.Seller]) {
			srows = append(srows, saleRow{sale: *s, Listing: s.Listing, line: len(listings) + j})
			addSeller(s.Listing.Seller)
		}
	}
	if o.Sellers > 0 && len(sellerOrder) > o.Sellers {
		allowed := map[string]bool{}
		for _, k := range sellerOrder[:o.Sellers] {
			allowed[k] = true
		}
		rows = slices.DeleteFunc(rows, func(r row) bool { return !allowed[r.Seller] })
		srows = slices.DeleteFunc(srows, func(s saleRow) bool { return !allowed[s.Listing.Seller] })
	}

	// People: everyone without filters, else the sellers and buyers kept.
	need := map[string]bool{}
	for i := range rows {
		r := &rows[i]
		need[r.Seller] = true
	}
	for i := range srows {
		s := &srows[i]
		need[s.Listing.Seller], need[s.sale.Buyer] = true, true
	}
	for i := range accounts {
		a := &accounts[i]
		_, communeOK := pl.places[a.Commune]
		if (!filtered || slices.Equal(o.Only, []string{OnlyAccounts}) || need[a.Key]) && communeOK {
			pl.accounts = append(pl.accounts, *a)
		} else if need[a.Key] {
			skipped["people in a commune the stack lacks"]++
		}
	}
	for i := range pl.accounts {
		a := &pl.accounts[i]
		pl.ids[a.Key] = p.Manifest.AccountID(a)
	}
	if err = l.dropTakenPhones(ctx, pl, skipped); err != nil {
		return nil, err
	}
	for i := range rows {
		r := &rows[i]
		if _, ok := pl.ids[r.Seller]; ok {
			r.id = p.Manifest.ListingID(&r.Listing)
			pl.onSale = append(pl.onSale, *r)
		}
	}
	for i := range srows {
		s := &srows[i]
		_, seller := pl.ids[s.sale.Listing.Seller]
		_, buyer := pl.ids[s.sale.Buyer]
		if seller && buyer {
			s.id = p.Manifest.ListingID(&s.Listing)
			pl.sales = append(pl.sales, *s)
		}
	}
	for _, f := range follows {
		_, a := pl.ids[f.Follower]
		_, b := pl.ids[f.Followee]
		if a && b {
			pl.followLines = append(pl.followLines, f)
		}
	}

	// Counts, and what is there already.
	if err = l.countThere(ctx, pl); err != nil {
		return nil, err
	}
	for i := range pl.onSale {
		r := &pl.onSale[i]
		pl.Photos += photoCount(r.Photos, o.FirstPhotoOnly)
	}
	for i := range pl.sales {
		s := &pl.sales[i]
		pl.Photos += photoCount(s.sale.Listing.Photos, o.FirstPhotoOnly)
		kept, dropped := l.d.Reviews.Keeps(s.sale.Reviews)
		pl.Reviews += kept
		pl.ReviewsDropped += dropped
	}
	pl.Follows = len(pl.followLines)
	pl.TextVectors, pl.textOn = vectorMode(o.Vectors, p.Manifest.Vectors.Text, l.d.TextVersion, p.Has(seedpack.TextVectors))
	pl.PhotoVectors, pl.photoOn = vectorMode(o.Vectors, p.Manifest.Vectors.Photo, l.d.ImageVersion, p.Has(seedpack.PhotoVectors))
	for reason, n := range skipped {
		pl.Skipped = append(pl.Skipped, fmt.Sprintf("%d %s", n, reason))
	}
	slices.Sort(pl.Skipped)
	return pl, nil
}

func photoCount(photos []string, first bool) int {
	if first {
		return min(len(photos), 1)
	}
	return len(photos)
}

// kept picks a sale for the history ratio by its key, the same on every run.
func kept(key string, ratio float64) bool {
	if ratio >= 1 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return float64(h.Sum32()%10_000) < ratio*10_000
}

// vectorMode says what the load does for one kind of vector, and whether
// it writes the pack's.
func vectorMode(mode string, set *seedpack.VectorSet, version string, file bool) (string, bool) {
	switch {
	case version == "":
		return "off in the stack", false
	case mode == VectorsNone:
		return "none", false
	case mode == VectorsRecompute:
		return "recompute", false
	case set == nil || !file:
		return "recompute (the pack has none)", false
	case set.Model != version:
		return fmt.Sprintf("recompute (the pack's %s is not the stack's %s)", set.Model, version), false
	case set.Origin != "":
		return fmt.Sprintf("pack (%s %s)", set.Origin, short(set.Model)), true
	}
	return fmt.Sprintf("pack (%s)", short(set.Model)), true
}

func short(model string) string {
	name, _, _ := strings.Cut(model, "@")
	return name
}

func places(ctx context.Context, regions catalogrepo.Regions, codes map[string]bool) (map[string]catalogrepo.Place, error) {
	list := make([]string, 0, len(codes))
	for c := range codes {
		list = append(list, c)
	}
	found, err := regions.Places(ctx, list)
	if err != nil {
		return nil, fmt.Errorf("read the communes: %w", err)
	}
	out := make(map[string]catalogrepo.Place, len(found))
	for _, f := range found {
		out[f.Code] = f
	}
	return out, nil
}

// dropTakenPhones leaves out the people whose phone another account holds,
// with their listings and sales.
func (l *Loader) dropTakenPhones(ctx context.Context, pl *Plan, skipped map[string]int) error {
	creds := accountrepo.NewCredentials(l.d.Pool)
	kept := pl.accounts[:0]
	for i := range pl.accounts {
		a := &pl.accounts[i]
		ref, err := creds.FindIdentity(ctx, "phone", a.Phone)
		switch {
		case err == nil && ref.AccountID != pl.ids[a.Key]:
			skipped["people whose phone another account holds"]++
			delete(pl.ids, a.Key)
			continue
		case err != nil && !isNotFound(err):
			return fmt.Errorf("look up phone of %s: %w", a.Key, err)
		default: // the phone is free, or the person's already
		}
		kept = append(kept, *a)
	}
	pl.accounts = kept
	return nil
}

// countThere fills the plan's counts and how many rows exist.
func (l *Loader) countThere(ctx context.Context, pl *Plan) error {
	ids := make([]uuid.UUID, 0, len(pl.accounts))
	for i := range pl.accounts {
		a := &pl.accounts[i]
		ids = append(ids, pl.ids[a.Key])
	}
	there, err := existing(ctx, l.d.Pool, "accounts", ids)
	if err != nil {
		return err
	}
	pl.People = Count{Total: len(ids), There: len(there)}
	ids = ids[:0]
	for i := range pl.onSale {
		r := &pl.onSale[i]
		ids = append(ids, r.id)
	}
	if there, err = existing(ctx, l.d.Pool, "listings", ids); err != nil {
		return err
	}
	pl.Listings = Count{Total: len(ids), There: len(there)}
	ids = ids[:0]
	for i := range pl.sales {
		s := &pl.sales[i]
		ids = append(ids, s.id)
	}
	if there, err = existing(ctx, l.d.Pool, "listings", ids); err != nil {
		return err
	}
	pl.Sold = Count{Total: len(ids), There: len(there)}
	return nil
}
