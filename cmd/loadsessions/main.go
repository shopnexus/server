// Command loadsessions signs the load test's accounts in on a dev stack and
// prints the sessions as JSON, which loadtest/ (k6) reads:
//
//	go run ./cmd/loadsessions -accounts 200 -sessions 2000 -trades 200 > loadtest/sessions.json
//
// Each signed-in k6 VU needs a session of its own, and a few hundred accounts
// can hold them all: session i belongs to account i mod -accounts, numbered
// +8490998xxxx. k6 rotates the refresh tokens it is given, so each run needs
// new sessions.
//
// A trade is a buyer and a seller session of the trade journey's VU, and one
// of the seller's listings. Its buyer, +8490997xxxx, is its own, so no two
// VUs move on one deal; its seller is one of -sellers, +8490996xxxx, each
// with -listings listings that are put on sale here, straight into the
// database: posting through the API would embed their text, a paid call.
// Deals and chats stay among these accounts, so no real person hears of them.
//
// After a run, -park hides the sellers' listings from everyone, and the
// next run puts them on sale again:
//
//	go run ./cmd/loadsessions -park
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/shopnexus/server/internal/account/devauth"
	"github.com/shopnexus/server/internal/account/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/postgres"
)

// maxAccounts is how many numbers each +849099xxxxxx range holds.
const maxAccounts = 10_000

// session is one signed-in device, the fields k6 needs.
type session struct {
	AccountID    string `json:"account_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"access_token_expires_at"`
}

// trade is one trade VU's two sides and the listing they deal on.
type trade struct {
	Buyer     session `json:"buyer"`
	Seller    session `json:"seller"`
	ListingID string  `json:"listing_id"`
}

type output struct {
	Sessions []session `json:"sessions"`
	Trades   []trade   `json:"trades"`
}

// The listings a seller puts on sale, in turn. Codes of the seeded categories
// and communes.
var wares = []catalogdomain.ListingInput{
	{Title: "Xe đạp Asama cũ", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed, CommuneCode: "00004"},
	{Title: "iPhone 12 64GB", Price: 6_200_000, CategoryCode: "phones.iphone", Condition: catalogdomain.ConditionLikeNew, CommuneCode: "25747"},
	{Title: "Giày chạy bộ size 42", Price: 650_000, CategoryCode: "fashion.shoes", Condition: catalogdomain.ConditionUsed, CommuneCode: "00004"},
}

func main() {
	accounts := flag.Int("accounts", 200, "accounts to sign in")
	sessions := flag.Int("sessions", 1000, "sessions to open across them")
	trades := flag.Int("trades", 0, "trades for the trade journey's VUs")
	sellers := flag.Int("sellers", 20, "seller accounts the trades share")
	listings := flag.Int("listings", 10, "listings each seller has on sale")
	park := flag.Bool("park", false, "hide the trade sellers' listings, and sign no one in")
	flag.Parse()
	var err error
	if *park {
		err = parkAll(context.Background())
	} else {
		err = run(context.Background(), *accounts, *sessions, *trades, *sellers, *listings)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadsessions:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, accounts, sessions, trades, sellers, listings int) error {
	switch {
	case accounts < 1 || accounts > maxAccounts:
		return fmt.Errorf("-accounts %d: want 1 to %d", accounts, maxAccounts)
	case sessions < 0:
		return fmt.Errorf("-sessions %d: want 0 or more", sessions)
	case trades < 0 || trades > maxAccounts:
		return fmt.Errorf("-trades %d: want 0 to %d", trades, maxAccounts)
	case trades > 0 && (sellers < 1 || sellers > maxAccounts || listings < 1):
		return fmt.Errorf("-sellers %d -listings %d: want at least one of each", sellers, listings)
	}
	cfg, err := config.FromDotEnv(".env")
	if err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()
	svc, err := devauth.Accounts(cfg, pool)
	if err != nil {
		return fmt.Errorf("account service: %w", err)
	}
	signIn := func(ctx context.Context, phone string) (session, error) {
		signed, err := svc.SignIn(ctx, service.SignInInput{
			Key: uuid.NewV7(), Proof: service.Proof{PhoneToken: phone}, DeviceName: "loadtest",
		})
		if err != nil {
			return session{}, fmt.Errorf("sign in %s: %w", phone, err)
		}
		return session{
			AccountID: signed.Account.ID.String(), AccessToken: signed.AccessToken,
			RefreshToken: signed.RefreshToken, ExpiresAt: signed.AccessTokenExpiresAt.Format(time.RFC3339),
		}, nil
	}

	out := output{Sessions: make([]session, sessions), Trades: make([]trade, trades)}
	member := func(ctx context.Context, i int) (err error) {
		out.Sessions[i], err = signIn(ctx, fmt.Sprintf("+8490998%04d", i%accounts))
		return err
	}
	// The first wave signs each account up once; two sign-ups of one number
	// at the same time would race.
	first := min(accounts, sessions)
	if err := parallel(ctx, 0, first, member); err != nil {
		return err
	}
	if err := parallel(ctx, first, sessions, member); err != nil {
		return err
	}

	if trades > 0 {
		sellers = min(sellers, trades)
		wares := make([][]uuid.UUID, sellers)
		seller := func(ctx context.Context, j int) error {
			s, err := signIn(ctx, fmt.Sprintf("+8490996%04d", j%sellers))
			if err != nil {
				return err
			}
			out.Trades[j].Seller = s
			if j < sellers {
				id, _ := uuid.Parse(s.AccountID)
				if wares[j], err = onSale(ctx, pool, id, listings); err != nil {
					return err
				}
			}
			return nil
		}
		if err := parallel(ctx, 0, sellers, seller); err != nil {
			return err
		}
		if err := parallel(ctx, sellers, trades, seller); err != nil {
			return err
		}
		if err := parallel(ctx, 0, trades, func(ctx context.Context, j int) (err error) {
			out.Trades[j].Buyer, err = signIn(ctx, fmt.Sprintf("+8490997%04d", j))
			out.Trades[j].ListingID = wares[j%sellers][(j/sellers)%listings].String()
			return err
		}); err != nil {
			return err
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("print sessions: %w", err)
	}
	return nil
}

// sellerPhones matches the trade sellers' numbers.
const sellerPhones = "+8490996%"

// parkAll hides every listing on sale of the trade sellers.
func parkAll(ctx context.Context) error {
	cfg, err := config.FromDotEnv(".env")
	if err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()
	tag, err := pool.Exec(ctx, `UPDATE listings SET status = 'hidden', updated_at = now()
WHERE status = 'active' AND seller_id IN (SELECT account_id FROM identities WHERE kind = 'phone' AND identifier LIKE @phones)`,
		pgx.StrictNamedArgs{"phones": sellerPhones})
	if err != nil {
		return fmt.Errorf("hide the sellers' listings: %w", err)
	}
	fmt.Fprintf(os.Stderr, "hid %d listings\n", tag.RowsAffected())
	return nil
}

// onSale returns n listings of seller's that take offers, putting the
// missing ones on sale first, so later runs reuse those of the first. Those
// -park hid go back on sale.
func onSale(ctx context.Context, pool *pgxpool.Pool, seller uuid.UUID, n int) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, `UPDATE listings SET status = 'active', updated_at = now()
WHERE id IN (SELECT id FROM listings WHERE seller_id = @seller AND status IN ('active', 'hidden') AND allow_offers ORDER BY id LIMIT @n)
RETURNING id`, pgx.StrictNamedArgs{"seller": seller, "n": n})
	if err != nil {
		return nil, fmt.Errorf("read the listings of %s: %w", seller, err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read the listings of %s: %w", seller, err)
	}
	listings := catalogrepo.NewListings(pool)
	for i := len(ids); i < n; i++ {
		d := wares[i%len(wares)]
		photo, now := uuid.NewV7(), time.Now()
		d.AllowOffers, d.Photos = true, []uuid.UUID{photo}
		l, err := catalogdomain.NewListing(uuid.NewV7(), seller, d, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, now)
		if err != nil {
			return nil, fmt.Errorf("build a listing: %w", err)
		}
		l.Published(map[uuid.UUID]catalogdomain.PhotoKeys{photo: {Photo: "loadtest/photo.jpg", Thumb: "loadtest/thumb.jpg"}}, now)
		if _, err = listings.Insert(ctx, l, uuid.Nil()); err != nil {
			return nil, fmt.Errorf("put a listing on sale: %w", err)
		}
		ids = append(ids, l.ID())
	}
	return ids, nil
}

// parallel runs f for each i in [from, to), 16 at a time.
func parallel(ctx context.Context, from, to int, f func(context.Context, int) error) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(16)
	for i := from; i < to; i++ {
		g.Go(func() error { return f(gctx, i) })
	}
	return g.Wait()
}
