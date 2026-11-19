package loader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountrepo "github.com/shopnexus/server/internal/account/repository"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatrepo "github.com/shopnexus/server/internal/chat/repository"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealrepo "github.com/shopnexus/server/internal/deal/repository"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	mediarepo "github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/seedpack"
)

// Result is what a load wrote.
type Result struct {
	People   Written  `json:"people"`
	Listings Written  `json:"listings"`
	Sold     Written  `json:"sold"`
	Reviews  int      `json:"reviews"`
	Follows  int      `json:"follows"`
	Objects  int64    `json:"objects"`
	Vectors  Vectors  `json:"vectors"`
	Warnings []string `json:"warnings,omitempty"`
}

// Written counts rows written and rows skipped, there already or refused.
type Written struct {
	Written int `json:"written"`
	Skipped int `json:"skipped"`
}

// Vectors counts the pack vectors written and the listings sent to
// ListingEmbed.
type Vectors struct {
	Text      int `json:"text"`
	Photo     int `json:"photo"`
	Refreshes int `json:"refreshes"`
}

// run is one load's state, shared by its workers.
type run struct {
	*Loader

	pl   *Plan
	prog seedpack.Progress
	name string

	accounts    accountrepo.Accounts
	credentials accountrepo.Credentials
	addresses   accountrepo.Addresses
	follows     accountrepo.Follows
	uploads     mediarepo.Uploads
	listings    catalogrepo.Listings
	chats       chatrepo.Chats
	deals       dealrepo.Deals

	mu      sync.Mutex
	res     Result
	objects atomic.Int64
	people  map[string]seedpack.Account
}

func (r *run) warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.mu.Lock()
	r.res.Warnings = append(r.res.Warnings, msg)
	r.mu.Unlock()
	r.prog.Warnf("%s", msg)
}

// Load writes people, sold items (before the on-sale cap counts listings),
// listings, follows and vectors. Rows already there are skipped, refused
// rows warn, and any other error stops the load.
func (l *Loader) Load(ctx context.Context, pl *Plan, prog seedpack.Progress) (Result, error) {
	db := l.d.Pool
	r := &run{
		Loader: l, pl: pl, prog: prog, name: pl.Pack.Name,
		accounts: accountrepo.NewAccounts(db), credentials: accountrepo.NewCredentials(db),
		addresses: accountrepo.NewAddresses(db), follows: accountrepo.NewFollows(db),
		uploads: mediarepo.NewUploads(db), listings: catalogrepo.NewListings(db),
		chats: chatrepo.NewChats(db), deals: dealrepo.NewDeals(db),
		people: map[string]seedpack.Account{},
	}
	for i := range pl.accounts {
		a := &pl.accounts[i]
		r.people[a.Key] = *a
	}
	workers := max(pl.opts.Workers, 1)

	there, err := existing(ctx, db, "accounts", idsOf(pl.accounts, func(a seedpack.Account) uuid.UUID { return pl.ids[a.Key] }))
	if err != nil {
		return r.res, err
	}
	if err = stage(ctx, prog, "people", workers, pl.accounts, func(ctx context.Context, a seedpack.Account) error {
		if there[pl.ids[a.Key]] {
			r.count(&r.res.People, false)
			return nil
		}
		return r.person(ctx, a)
	}); err != nil {
		return r.res, err
	}

	ids := append(idsOf(pl.sales, func(s saleRow) uuid.UUID { return s.id }), idsOf(pl.onSale, func(x row) uuid.UUID { return x.id })...)
	if there, err = existing(ctx, db, "listings", ids); err != nil {
		return r.res, err
	}
	if err = stage(ctx, prog, "sold", workers, pl.sales, func(ctx context.Context, s saleRow) error {
		if there[s.id] {
			r.count(&r.res.Sold, false)
			return nil
		}
		return r.sale(ctx, s)
	}); err != nil {
		return r.res, err
	}
	if err = stage(ctx, prog, "listings", workers, pl.onSale, func(ctx context.Context, x row) error {
		if there[x.id] {
			r.count(&r.res.Listings, false)
			return nil
		}
		return r.listing(ctx, x)
	}); err != nil {
		return r.res, err
	}
	if err = stage(ctx, prog, "follows", workers, pl.followLines, r.follow); err != nil {
		return r.res, err
	}
	if err = r.vectors(ctx); err != nil {
		return r.res, err
	}
	r.res.Objects = r.objects.Load()
	return r.res, nil
}

func (r *run) count(w *Written, written bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if written {
		w.Written++
	} else {
		w.Skipped++
	}
}

// stage runs fn on every item with workers at once, under a progress bar.
func stage[T any](ctx context.Context, prog seedpack.Progress, name string, workers int, items []T, fn func(context.Context, T) error) error {
	if len(items) == 0 {
		return nil
	}
	bar := prog.Stage(name, int64(len(items)))
	defer bar.Done()
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for _, it := range items {
		g.Go(func() error {
			if err := fn(gctx, it); err != nil {
				return err
			}
			bar.Add(1)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// person signs the person up with their phone, then sets their bio,
// avatar, last activity and address.
func (r *run) person(ctx context.Context, a seedpack.Account) error {
	id, at := r.pl.ids[a.Key], a.JoinedAt
	acc, err := accountdomain.New(id, a.Name, at)
	if err == nil {
		err = acc.Edit(&a.Name, &a.Bio, at)
	}
	if err != nil {
		r.warnf("person %s: %v", a.Key, err)
		r.count(&r.res.People, false)
		return nil
	}
	phone, err := contact.ParsePhone(a.Phone)
	if err != nil {
		r.warnf("person %s: phone %s: %v", a.Key, a.Phone, err)
		r.count(&r.res.People, false)
		return nil
	}
	creds := accountdomain.NewCredentials(id, accountdomain.NewIdentity(seedpack.ID(r.name, "identity", a.Key, at), phone, at), at)
	if err = r.credentials.SignUp(ctx, acc, creds); errors.Is(err, accountdomain.ErrIdentityTaken) {
		r.warnf("person %s: another account holds %s", a.Key, a.Phone)
		r.count(&r.res.People, false)
		return nil
	} else if err != nil {
		return fmt.Errorf("sign up %s: %w", a.Key, err)
	}
	if a.Avatar != "" {
		square, readErr := r.pl.src.ReadFile(seedpack.AvatarPath(a.Avatar))
		if readErr != nil {
			return readErr
		}
		upload := seedpack.ID(r.name, "avatar", a.Key, at)
		key, upErr := r.upload(ctx, upload, id, id, mediadomain.ShapeSquare, square, nil, at)
		if upErr != nil {
			return fmt.Errorf("avatar of %s: %w", a.Key, upErr)
		}
		acc.SetAvatar(key, at)
	}
	if a.Bio != "" || a.Avatar != "" {
		if err = r.accounts.Save(ctx, &acc, seedpack.ID(r.name, "account-save", a.Key, at), nil); err != nil {
			return fmt.Errorf("save %s: %w", a.Key, err)
		}
	}
	if a.LastActiveAt != nil {
		if err = r.accounts.TouchActive(ctx, id, *a.LastActiveAt); err != nil {
			return fmt.Errorf("last activity of %s: %w", a.Key, err)
		}
	}
	if ad := a.Address; ad != nil {
		_, communeOK := r.pl.places[ad.Commune]
		addr, err := accountdomain.NewAddress(seedpack.ID(r.name, "address", a.Key, at), id, accountdomain.AddressDraft{
			Name: a.Name, Phone: phone.Value(), Street: ad.Street, CommuneCode: ad.Commune, Kind: accountdomain.AddressKind(ad.Kind),
		}, communeOK, at)
		if err != nil {
			r.warnf("address of %s: %v", a.Key, err)
		} else if err = r.addresses.Insert(ctx, seedpack.ID(r.name, "address-key", a.Key, at), &addr, true); err != nil {
			return fmt.Errorf("address of %s: %w", a.Key, err)
		}
	}
	r.count(&r.res.People, true)
	return nil
}

// upload puts a processed photo (and its thumbnail, for a listing photo)
// at the keys MediaProcess gives them and stores the upload as processed
// for holder. It returns the photo's key.
func (r *run) upload(ctx context.Context, id, owner, holder uuid.UUID, shape mediadomain.Shape, photo, thumb []byte, at time.Time) (string, error) {
	photoKey := "media/" + id.String() + ".jpg"
	if err := r.d.Store.Put(ctx, photoKey, "image/jpeg", bytes.NewReader(photo), int64(len(photo))); err != nil {
		return "", fmt.Errorf("put %s: %w", photoKey, err)
	}
	r.objects.Add(1)
	thumbKey := ""
	if thumb != nil {
		thumbKey = "media/" + id.String() + "_thumb.jpg"
		if err := r.d.Store.Put(ctx, thumbKey, "image/jpeg", bytes.NewReader(thumb), int64(len(thumb))); err != nil {
			return "", fmt.Errorf("put %s: %w", thumbKey, err)
		}
		r.objects.Add(1)
	}
	u, err := mediadomain.NewUpload(id, owner, "image/jpeg", int64(len(photo)), mediadomain.OriginApp, at)
	if err != nil {
		return "", fmt.Errorf("upload %s: %w", id, err)
	}
	if err = r.uploads.Create(ctx, u); err != nil {
		return "", fmt.Errorf("create upload: %w", err)
	}
	if _, err = u.Attach(owner, holder, shape, at); err != nil {
		return "", fmt.Errorf("attach upload: %w", err)
	}
	u.Processed(photoKey, thumbKey, at)
	if err = r.uploads.Save(ctx, &u, seedpack.ID(r.name, "upload-save", id.String(), at)); err != nil && !errors.Is(err, mediarepo.ErrConflict) {
		return "", fmt.Errorf("save upload: %w", err)
	}
	return photoKey, nil
}

// build makes the listing entity, published at its creation, after
// uploading its photos; nil when the server's rules refuse it.
func (r *run) build(ctx context.Context, x row) (*catalogdomain.Listing, error) {
	seller := r.pl.ids[x.Seller]
	photos := x.Photos
	if r.pl.opts.FirstPhotoOnly && len(photos) > 1 {
		photos = photos[:1]
	}
	in := x.Input()
	in.Photos = make([]uuid.UUID, len(photos))
	keys := map[uuid.UUID]catalogdomain.PhotoKeys{}
	for i, sha := range photos {
		photo, err := r.pl.src.ReadFile(seedpack.PhotoPath(sha))
		if err != nil {
			return nil, err
		}
		thumb, err := r.pl.src.ReadFile(seedpack.ThumbPath(sha))
		if err != nil {
			return nil, err
		}
		id := seedpack.ID(r.name, "upload", fmt.Sprintf("%s/%d", x.Key, i), x.CreatedAt)
		key, err := r.upload(ctx, id, seller, x.id, mediadomain.ShapePhoto, photo, thumb, x.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("photo %d of %s: %w", i, x.Key, err)
		}
		in.Photos[i] = id
		keys[id] = catalogdomain.PhotoKeys{Photo: key, Thumb: "media/" + id.String() + "_thumb.jpg"}
	}
	place := r.pl.places[x.Commune]
	refsIn := catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true, CommunePoint: catalogdomain.Point{Lat: place.Lat, Lng: place.Lng}}
	l, err := catalogdomain.NewListing(x.id, seller, in, refsIn, x.CreatedAt)
	if err != nil {
		r.warnf("listing %s: %v", x.Key, err)
		return nil, nil //nolint:nilnil // a refused listing is skipped, not an error
	}
	l.Published(keys, x.CreatedAt)
	return &l, nil
}

// listing posts one listing on sale, or hidden.
func (r *run) listing(ctx context.Context, x row) error {
	l, err := r.build(ctx, x)
	if err != nil || l == nil {
		r.count(&r.res.Listings, false)
		return err
	}
	if x.Status == seedpack.StatusHidden {
		if err = l.Hide(x.CreatedAt); err != nil {
			return fmt.Errorf("hide %s: %w", x.Key, err)
		}
	}
	if err = r.listings.Insert(ctx, *l); errors.Is(err, catalogdomain.ErrCapReached) {
		r.warnf("listing %s: its seller has %d listings on sale already", x.Key, catalogdomain.MaxOnSale)
		r.count(&r.res.Listings, false)
		return nil
	} else if err != nil {
		return fmt.Errorf("insert %s: %w", x.Key, err)
	}
	r.count(&r.res.Listings, true)
	return nil
}

// sale writes a sold item as the app makes one: the listing, the pair's
// conversation, a deal with a delivered COD order, the listing sold to it,
// then the reviews.
func (r *run) sale(ctx context.Context, s saleRow) error {
	l, err := r.build(ctx, s.row)
	if err != nil || l == nil {
		r.count(&r.res.Sold, false)
		return err
	}
	seller, buyer := r.pl.ids[s.sale.Listing.Seller], r.pl.ids[s.sale.Buyer]
	shipped := s.sale.OrderedAt.Add(s.sale.DeliveredAt.Sub(s.sale.OrderedAt) / 3)
	if s.sale.ShippedAt != nil {
		shipped = *s.sale.ShippedAt
	}
	completed := s.sale.DeliveredAt.Add(time.Hour)

	conv := chatdomain.NewConversation(seedpack.ID(r.name, "conversation", s.sale.Listing.Seller+"|"+s.sale.Buyer, s.sale.OrderedAt), seller, buyer, s.sale.OrderedAt)
	// The deal goes through its rules once before anything is written, and
	// again in the pair's conversation, which may be an older one.
	d, err := r.deal(s, conv.ID, shipped, completed)
	if err == nil {
		order := d.Order().ID
		if _, err = l.Reserve(order, shipped); err == nil && !l.SellTo(order, completed) {
			err = errors.New("the listing was not held for the order")
		}
	}
	if err != nil {
		r.warnf("sale %s: %v", s.sale.Listing.Key, err)
		r.count(&r.res.Sold, false)
		return nil
	}
	if err = r.listings.Insert(ctx, *l); errors.Is(err, catalogdomain.ErrCapReached) {
		r.warnf("sale %s: its seller has %d listings on sale already", s.sale.Listing.Key, catalogdomain.MaxOnSale)
		r.count(&r.res.Sold, false)
		return nil
	} else if err != nil {
		return fmt.Errorf("insert %s: %w", s.sale.Listing.Key, err)
	}
	if conv, err = r.chats.OpenPair(ctx, conv); err != nil {
		return fmt.Errorf("conversation of %s: %w", s.sale.Listing.Key, err)
	}
	if d, err = r.deal(s, conv.ID, shipped, completed); err != nil {
		return fmt.Errorf("deal of %s: %w", s.sale.Listing.Key, err)
	}
	inserted, err := r.deals.Insert(ctx, d)
	if err != nil {
		return fmt.Errorf("deal of %s: %w", s.sale.Listing.Key, err)
	}
	order := d.Order().ID
	if !inserted {
		// A load stopped after the deal; its order and reviews may be there.
		got, findErr := r.deals.Find(ctx, s.id, buyer)
		if findErr != nil {
			return fmt.Errorf("deal of %s: %w", s.sale.Listing.Key, findErr)
		}
		if o := got.Order(); o != nil {
			order = o.ID
		}
	} else if err = r.deals.Save(ctx, &d, seedpack.ID(r.name, "deal-save", s.sale.Listing.Key, completed)); err != nil {
		return fmt.Errorf("deal of %s: %w", s.sale.Listing.Key, err)
	}
	n, err := r.d.Reviews.Write(ctx, Sold{
		Pack: r.name, ListingKey: s.sale.Listing.Key, ListingID: s.id, OrderID: order, SellerID: seller, BuyerID: buyer, ClosedAt: completed,
	}, s.sale.Reviews)
	if err != nil {
		return fmt.Errorf("reviews of %s: %w", s.sale.Listing.Key, err)
	}
	r.mu.Lock()
	r.res.Reviews += n
	r.mu.Unlock()
	r.count(&r.res.Sold, true)
	return nil
}

// deal runs a sale's deal through the server's rules: agreed, a COD order
// to the buyer's address, shipped by GHTK, delivered and confirmed.
func (r *run) deal(s saleRow, conversation uuid.UUID, shipped, completed time.Time) (dealdomain.Deal, error) {
	seller, buyer := r.pl.ids[s.sale.Listing.Seller], r.pl.ids[s.sale.Buyer]
	d := dealdomain.Open(seedpack.ID(r.name, "deal", s.sale.Listing.Key, s.sale.OrderedAt), conversation, s.id, buyer, seller, s.sale.OrderedAt.Add(-2*time.Hour))
	dl := dealdomain.Listing{Price: s.sale.Listing.Price, AllowOffers: s.sale.Listing.AllowOffers, Status: dealdomain.ListingActive}
	if err := agree(&d, dl, s.sale.Price, s.sale.OrderedAt); err != nil {
		return d, err
	}
	b := r.people[s.sale.Buyer]
	to := dealdomain.Recipient{Name: b.Name, Phone: b.Phone, Street: "Giao tại nhà", CommuneCode: b.Commune, Kind: dealdomain.AddressHome}
	if ad := b.Address; ad != nil {
		to.Street, to.CommuneCode, to.Kind = ad.Street, ad.Commune, dealdomain.AddressKind(ad.Kind)
	}
	if err := d.RequestCod(dealdomain.Buyer, uuid.NewV7(), to, dl, s.sale.OrderedAt); err != nil {
		return d, fmt.Errorf("request COD: %w", err)
	}
	order := d.Order().ID
	err := d.Ship(dealdomain.Seller, order, dealdomain.Shipment{
		Mode: dealdomain.ModeManual, Carrier: dealdomain.CarrierGHTK, Status: dealdomain.ShipmentInTransit,
		TrackingCode: "SEED" + order.String()[24:],
	}, dl, shipped)
	if err != nil {
		return d, fmt.Errorf("ship: %w", err)
	}
	if _, err = d.ShipmentUpdated(order, dealdomain.ShipmentDelivered, s.sale.DeliveredAt); err != nil {
		return d, fmt.Errorf("deliver: %w", err)
	}
	if err = d.ConfirmReceipt(dealdomain.Buyer, order, completed); err != nil {
		return d, fmt.Errorf("confirm receipt: %w", err)
	}
	return d, nil
}

// agree brings the deal to the sale's price: the buyer offers it and the
// seller accepts, or the buyer takes the listed price when no offer fits.
func agree(d *dealdomain.Deal, dl dealdomain.Listing, price int64, ordered time.Time) error {
	if price < dl.Price && dl.AllowOffers {
		if rng, ok := d.OfferRange(dealdomain.Buyer, dl); ok {
			amount := min(max(price/dealdomain.Step*dealdomain.Step, rng.Min), rng.Max)
			if err := d.Offer(dealdomain.Buyer, amount, dl, ordered.Add(-90*time.Minute)); err != nil {
				return fmt.Errorf("offer: %w", err)
			}
			if err := d.Accept(dealdomain.Seller, amount, dl, ordered.Add(-time.Hour)); err != nil {
				return fmt.Errorf("accept: %w", err)
			}
			return nil
		}
	}
	if err := d.Accept(dealdomain.Buyer, dl.Price, dl, ordered.Add(-time.Hour)); err != nil {
		return fmt.Errorf("accept: %w", err)
	}
	return nil
}

func (r *run) follow(ctx context.Context, f seedpack.Follow) error {
	id := seedpack.ID(r.name, "follow", f.Follower+">"+f.Followee, f.At)
	already, err := r.follows.Following(ctx, r.pl.ids[f.Follower], r.pl.ids[f.Followee])
	if err != nil {
		return fmt.Errorf("follow %s → %s: %w", f.Follower, f.Followee, err)
	}
	if already {
		return nil
	}
	err = r.follows.Follow(ctx, id, r.pl.ids[f.Follower], r.pl.ids[f.Followee], f.At)
	if errors.Is(err, accountdomain.ErrBlocked) || errors.Is(err, accountdomain.ErrFollowLimit) {
		r.warnf("follow %s → %s: %v", f.Follower, f.Followee, err)
		return nil
	}
	if err != nil {
		return fmt.Errorf("follow %s → %s: %w", f.Follower, f.Followee, err)
	}
	r.mu.Lock()
	r.res.Follows++
	r.mu.Unlock()
	return nil
}

func idsOf[T any](items []T, id func(T) uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, len(items))
	for i, it := range items {
		out[i] = id(it)
	}
	return out
}

// existing returns which of ids have a row in table, accounts or listings.
// A read of IDs only; no repository lists them in bulk.
func existing(ctx context.Context, db *pgxpool.Pool, table string, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	if !slices.Contains([]string{"accounts", "listings"}, table) {
		return nil, fmt.Errorf("existing: unknown table %s", table)
	}
	out := map[uuid.UUID]bool{}
	for chunk := range slices.Chunk(ids, 5000) {
		rows, err := db.Query(ctx, `SELECT id FROM `+table+` WHERE id = ANY(@ids)`, pgx.StrictNamedArgs{"ids": chunk})
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", table, err)
		}
		found, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", table, err)
		}
		for _, id := range found {
			out[id] = true
		}
	}
	return out, nil
}

func isNotFound(err error) bool { return errors.Is(err, accountrepo.ErrNotFound) }
