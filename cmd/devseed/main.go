// Command devseed fills a development database with people, listings and a
// few deals, so the app has something to show. It runs against the server
// that make run started and refuses any APP_ENV but dev.
//
//	go run ./cmd/devseed
//
// Each person signs in by phone through Account.SignIn, the path the app
// takes, except that the phone token is the number itself: devseed builds
// Account with a verifier that trusts it. Everything after sign-in goes
// through the HTTP API with that person's token. The people, listings and
// photos come from data/, built from the app's fixtures.
//
// A second run adds only what is missing: a profile without a name, a
// person without an address, a listing whose title its seller does not
// have, and the deals while the first buyer's inbox is empty.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	"uuid"

	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

//go:embed data/seed.json data/photos
var data embed.FS

// seed is data/seed.json.
type seed struct {
	Accounts []person  `json:"accounts"`
	Listings []listing `json:"listings"`
}

type person struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Bio     string `json:"bio"`
	Avatar  string `json:"avatar"`
	Street  string `json:"street"`
	Commune string `json:"commune"`
	Carrier bool   `json:"carrier"`
}

type listing struct {
	Key         string   `json:"key"`
	Seller      string   `json:"seller"`
	Title       string   `json:"title"`
	Price       int64    `json:"price"`
	Category    string   `json:"category"`
	Condition   string   `json:"condition"`
	AllowOffers bool     `json:"allow_offers"`
	Commune     string   `json:"commune"`
	Photos      []string `json:"photos"`
	Specs       []spec   `json:"specs"`
	Description string   `json:"description"`
}

type spec struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// member is a seeded person once signed in.
type member struct {
	person

	id    string
	token string
}

// phoneAsToken proves the phone number its token spells. Only devseed uses
// it; the server checks phone tokens with Firebase.
type phoneAsToken struct{}

func (phoneAsToken) Verify(_ context.Context, token string) (contact.Address, error) {
	addr, err := contact.ParsePhone(token)
	if err != nil {
		return contact.Address{}, fmt.Errorf("phone token %q: %w", token, err)
	}
	return addr, nil
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
	s, err := load()
	if err != nil {
		return err
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
	accounts := service.New(service.Deps{
		Accounts: repository.NewAccounts(pool), Credentials: repository.NewCredentials(pool), Sessions: repository.NewSessions(pool),
		Signer: domain.NewSigner(cfg.Auth), Phones: phoneAsToken{}, Ingress: ingress.NewClient(cfg.Restate.IngressURL),
		Prefix: cfg.Restate.ServicePrefix, Now: time.Now, Log: slog.Default(),
	})

	// People: sign in, then a name, a photo and an address for whoever has
	// none, and GHTK through the fake carrier for every seller.
	people := map[string]*member{}
	for i := range s.Accounts {
		p := &s.Accounts[i]
		signed, signErr := accounts.SignIn(ctx, service.SignInInput{
			IdempotencyKey: uuid.NewV7().String(), Proof: service.Proof{PhoneToken: p.Phone}, DeviceName: "devseed",
		})
		if signErr != nil {
			return fmt.Errorf("sign in %s: %w", p.Key, signErr)
		}
		m := &member{person: *p, id: signed.Account.ID.String(), token: signed.AccessToken}
		people[p.Key] = m
		var me struct {
			DisplayName string `json:"display_name"`
		}
		if err = c.call(ctx, m.token, http.MethodGet, "/v1/accounts/me", nil, &me); err != nil {
			return fmt.Errorf("account of %s: %w", p.Key, err)
		}
		if me.DisplayName == "" {
			patch := map[string]any{"display_name": p.Name, "bio": p.Bio}
			if p.Avatar != "" {
				id, upErr := c.upload(ctx, m.token, "data/photos/"+p.Avatar)
				if upErr != nil {
					return fmt.Errorf("avatar of %s: %w", p.Key, upErr)
				}
				patch["avatar_upload_id"] = id
			}
			if err = c.call(ctx, m.token, http.MethodPatch, "/v1/accounts/me", patch, nil); err != nil {
				return fmt.Errorf("profile of %s: %w", p.Key, err)
			}
		}
		if _, err = c.address(ctx, m); err != nil {
			return fmt.Errorf("address of %s: %w", p.Key, err)
		}
		if p.Carrier {
			login := map[string]any{"email": strings.TrimPrefix(p.Phone, "+") + "@devseed.test", "password": "devseed"}
			if err = c.call(ctx, m.token, http.MethodPut, "/v1/carrier-accounts/ghtk", login, nil); err != nil {
				return fmt.Errorf("link GHTK for %s: %w", p.Key, err)
			}
		}
	}
	fmt.Printf("%d people signed in\n", len(people))

	// Listings: post each one its seller does not have by title, then wait
	// until every one has left processing.
	posted, ids := 0, map[string]string{}
	for i := range s.Listings {
		l := &s.Listings[i]
		seller := people[l.Seller]
		var mine struct {
			Items []struct{ ID, Title string } `json:"items"`
		}
		if err = c.call(ctx, seller.token, http.MethodGet, "/v1/me/listings?pane=on_sale&limit=100", nil, &mine); err != nil {
			return fmt.Errorf("listings of %s: %w", l.Seller, err)
		}
		for _, it := range mine.Items {
			if it.Title == l.Title {
				ids[l.Key] = it.ID
			}
		}
		if ids[l.Key] != "" {
			continue
		}
		photos := make([]string, 0, len(l.Photos))
		for _, f := range l.Photos {
			id, upErr := c.upload(ctx, seller.token, "data/photos/"+f)
			if upErr != nil {
				return fmt.Errorf("photo %s of %s: %w", f, l.Key, upErr)
			}
			photos = append(photos, id)
		}
		var out struct{ ID string }
		err = c.call(ctx, seller.token, http.MethodPost, "/v1/listings", map[string]any{
			"title": l.Title, "price": l.Price, "description": l.Description, "category_code": l.Category,
			"condition": l.Condition, "allow_offers": l.AllowOffers, "commune_code": l.Commune, "photos": photos, "specs": l.Specs,
		}, &out, "Idempotency-Key", uuid.NewV7().String())
		if err != nil {
			return fmt.Errorf("post %s: %w", l.Key, err)
		}
		ids[l.Key] = out.ID
		posted++
	}
	deadline := time.Now().Add(3 * time.Minute)
	for i := range s.Listings {
		l := &s.Listings[i]
		for {
			var got struct{ Status string }
			if err = c.call(ctx, people[l.Seller].token, http.MethodGet, "/v1/listings/"+ids[l.Key], nil, &got); err != nil {
				return fmt.Errorf("listing %s: %w", l.Key, err)
			}
			if got.Status == "rejected" {
				return fmt.Errorf("listing %s was rejected; the server log says why its photos failed", l.Key)
			}
			if got.Status != "processing" {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("listing %s is still processing after 3 minutes", l.Key)
			}
			time.Sleep(time.Second)
		}
	}
	fmt.Printf("%d listings posted, %d were there\n", posted, len(ids)-posted)

	// Deals, while Khánh Linh's inbox is empty: she saves three listings,
	// follows Tuấn Kiệt, asks about and offers on his iPhone 13 Pro, and buys
	// Hoàng Nam's iPhone 12 at its price by COD, which Nam ships by GHTK.
	linh, kiet, nam := people["u-linh"], people["u-kiet"], people["u-nam"]
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

// load reads data/seed.json.
func load() (seed, error) {
	raw, err := data.ReadFile("data/seed.json")
	if err != nil {
		return seed{}, fmt.Errorf("read data/seed.json: %w", err)
	}
	var s seed
	if err = json.Unmarshal(raw, &s); err != nil {
		return seed{}, fmt.Errorf("data/seed.json: %w", err)
	}
	return s, nil
}

// address returns the id of the person's first address, adding their
// seeded one when the book is empty.
func (c client) address(ctx context.Context, m *member) (string, error) {
	var book struct {
		Items []struct{ ID string } `json:"items"`
	}
	if err := c.call(ctx, m.token, http.MethodGet, "/v1/addresses", nil, &book); err != nil {
		return "", err
	}
	if len(book.Items) > 0 {
		return book.Items[0].ID, nil
	}
	var made struct{ ID string }
	err := c.call(ctx, m.token, http.MethodPost, "/v1/addresses", map[string]any{
		"name": m.Name, "phone": m.Phone, "street": m.Street, "commune_code": m.Commune,
	}, &made, "Idempotency-Key", uuid.NewV7().String())
	if err != nil {
		return "", fmt.Errorf("add the address: %w", err)
	}
	return made.ID, nil
}
