// Command devseed loads the demo pack into a dev stack, signs its people in
// with a verifier that trusts the phone number, and makes a few deals
// through the HTTP API. A second run adds only what is missing.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"uuid"

	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/devauth"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/seedpack"
	"github.com/shopnexus/server/internal/seedpack/loader"
)

// packDir is the demo pack, from the repository's root.
const packDir = seedpack.RepoPacks + "/demo"

// member is a pack person once signed in.
type member struct {
	seedpack.Account

	id    string
	token string
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "devseed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.FromDotEnv(".env")
	if err != nil {
		return err
	}
	if cfg.Env != config.EnvDev {
		return fmt.Errorf("APP_ENV is %s; devseed only fills a dev database", cfg.Env)
	}
	c := client{base: "http://127.0.0.1" + cfg.HTTPAddr[strings.LastIndex(cfg.HTTPAddr, ":"):], http: &http.Client{Timeout: 30 * time.Second}}
	if err = c.call(ctx, "", http.MethodGet, "/readyz", nil, nil); err != nil {
		return fmt.Errorf("the server is not ready, start it with make run: %w", err)
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()
	store, err := objectstore.New(cfg.ObjectStore)
	if err != nil {
		return fmt.Errorf("connect to the object store: %w", err)
	}

	// The pack: people, listings, sold items and follows.
	p, err := seedpack.Open(packDir)
	if err != nil {
		return err
	}
	d := loader.Deps{Pool: pool, Store: store, Ingress: ingress.NewClient(cfg.Restate.IngressURL), Prefix: cfg.Restate.ServicePrefix}
	if cfg.Embed.TextOn() {
		d.TextVersion = cfg.Embed.TextVersion
	}
	if cfg.AI.EmbedOn() {
		d.ImageVersion = cfg.Embed.ImageVersion
	}
	l := loader.New(d)
	plan, err := l.Plan(ctx, p, loader.Options{Vectors: loader.VectorsPack, HistoryRatio: 1, Workers: 4, EmbedEvery: time.Second})
	if err != nil {
		return err
	}
	loaded, err := l.Load(ctx, plan, seedpack.Discard{})
	if err != nil {
		return fmt.Errorf("load %s: %w", packDir, err)
	}
	for _, w := range loaded.Warnings {
		fmt.Println("  !", w)
	}
	fmt.Printf("demo pack: %d people, %d listings and %d sold items written, %d people and %d listings were there\n",
		loaded.People.Written, loaded.Listings.Written, loaded.Sold.Written, loaded.People.Skipped, loaded.Listings.Skipped)

	// Sign everyone in, and link GHTK through the fake carrier for every
	// seller.
	accounts, err := devauth.Accounts(cfg, pool)
	if err != nil {
		return fmt.Errorf("account service: %w", err)
	}
	people, err := p.Accounts()
	if err != nil {
		return err
	}
	listings, err := p.Listings()
	if err != nil {
		return err
	}
	sells := map[string]bool{}
	ids := map[string]string{}
	for i := range listings {
		x := &listings[i]
		sells[x.Seller] = true
		ids[x.Key] = p.Manifest.ListingID(x).String()
	}
	members := map[string]*member{}
	for i := range people {
		a := &people[i]
		signed, signErr := accounts.SignIn(ctx, service.SignInInput{
			IdempotencyKey: uuid.NewV7().String(), Proof: service.Proof{PhoneToken: a.Phone}, DeviceName: "devseed",
		})
		if signErr != nil {
			return fmt.Errorf("sign in %s: %w", a.Key, signErr)
		}
		if signed.Account.ID != p.Manifest.AccountID(a) {
			return fmt.Errorf("%s signs in as %s, not the pack's person; seedpack wipe demo, or free the number", a.Phone, signed.Account.ID)
		}
		m := &member{Account: *a, id: signed.Account.ID.String(), token: signed.AccessToken}
		members[a.Key] = m
		if sells[a.Key] {
			login := map[string]any{"email": strings.TrimPrefix(a.Phone, "+") + "@devseed.test", "password": "devseed"}
			if err = c.call(ctx, m.token, http.MethodPut, "/v1/carrier-accounts/ghtk", login, nil); err != nil {
				return fmt.Errorf("link GHTK for %s: %w", a.Key, err)
			}
		}
	}
	fmt.Printf("%d people signed in\n", len(members))

	// Deals, while Khánh Linh's inbox is empty: she saves three listings,
	// follows Tuấn Kiệt, asks about and offers on his iPhone 13 Pro, and buys
	// Hoàng Nam's iPhone 12 at its price by COD, which Nam ships by GHTK.
	linh, kiet, nam := members["u-linh"], members["u-kiet"], members["u-nam"]
	var inbox struct {
		Items []json.RawMessage `json:"items"`
	}
	if err = c.call(ctx, linh.token, http.MethodGet, "/v1/screens/inbox", nil, &inbox); err != nil {
		return fmt.Errorf("inbox: %w", err)
	}
	if len(inbox.Items) > 0 {
		fmt.Println("deals were there")
		return nil
	}
	for _, key := range []string{"p-iphone14", "p-sofa", "p-a7"} {
		if err = c.call(ctx, linh.token, http.MethodPut, "/v1/saved/"+ids[key], nil, nil); err != nil {
			return fmt.Errorf("save %s: %w", key, err)
		}
	}
	if err = c.call(ctx, linh.token, http.MethodPost, "/v1/follows", map[string]any{"account_id": kiet.id}, nil); err != nil {
		return fmt.Errorf("follow: %w", err)
	}
	var conv struct{ ID string }
	err = c.call(ctx, linh.token, http.MethodPost, "/v1/conversations", map[string]any{
		"listing_id": ids["p-iphone13"], "text": "Chào anh, máy còn không ạ? Pin có thay lần nào chưa anh?",
	}, &conv, "Idempotency-Key", uuid.NewV7().String())
	if err != nil {
		return fmt.Errorf("open chat: %w", err)
	}
	err = c.call(ctx, kiet.token, http.MethodPost, "/v1/conversations/"+conv.ID+"/messages", map[string]any{
		"text": "Còn bạn nhé. Máy zin, chưa thay pin, Face ID nhạy.",
	}, nil, "Idempotency-Key", uuid.NewV7().String())
	if err != nil {
		return fmt.Errorf("reply: %w", err)
	}
	var offered struct{ ID string }
	if err = c.call(ctx, linh.token, http.MethodPost, "/v1/deals", map[string]any{"listing_id": ids["p-iphone13"]}, &offered); err != nil {
		return fmt.Errorf("open deal: %w", err)
	}
	if err = c.call(ctx, linh.token, http.MethodPost, "/v1/deals/"+offered.ID+"/offer", map[string]any{"amount": 4_800_000}, nil); err != nil {
		return fmt.Errorf("offer: %w", err)
	}
	var bought struct {
		ID    string
		Order struct{ ID string }
	}
	if err = c.call(ctx, linh.token, http.MethodPost, "/v1/deals", map[string]any{"listing_id": ids["p-iphone12"]}, &bought); err != nil {
		return fmt.Errorf("open the second deal: %w", err)
	}
	if err = c.call(ctx, linh.token, http.MethodPost, "/v1/deals/"+bought.ID+"/accept", map[string]any{"price": 6_900_000}, nil); err != nil {
		return fmt.Errorf("accept the price: %w", err)
	}
	address, err := c.address(ctx, linh)
	if err != nil {
		return err
	}
	if err = c.call(ctx, linh.token, http.MethodPost, "/v1/deals/"+bought.ID+"/cod", map[string]any{"address_id": address}, &bought); err != nil {
		return fmt.Errorf("request COD: %w", err)
	}
	err = c.call(ctx, nam.token, http.MethodPost, "/v1/orders/"+bought.Order.ID+"/shipment", map[string]any{
		"mode": "manual", "carrier": "ghtk", "tracking_code": "SEED" + strings.ToUpper(bought.Order.ID[len(bought.Order.ID)-8:]),
	}, nil, "Idempotency-Key", uuid.NewV7().String())
	if err != nil {
		return fmt.Errorf("ship: %w", err)
	}
	fmt.Println("deals made: Khánh Linh offers on Tuấn Kiệt's iPhone 13 Pro and has Hoàng Nam's iPhone 12 shipped")
	return nil
}

// address returns the id of the person's first address, which the pack
// gave them.
func (c client) address(ctx context.Context, m *member) (string, error) {
	var book struct {
		Items []struct{ ID string } `json:"items"`
	}
	if err := c.call(ctx, m.token, http.MethodGet, "/v1/addresses", nil, &book); err != nil {
		return "", err
	}
	if len(book.Items) == 0 {
		return "", fmt.Errorf("%s has no address; the demo pack gives everyone one", m.Key)
	}
	return book.Items[0].ID, nil
}
