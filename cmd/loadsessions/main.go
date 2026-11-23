// Command loadsessions signs the load test's accounts in on a dev stack and
// prints the sessions as JSON, which loadtest/ (k6) reads:
//
//	go run ./cmd/loadsessions -accounts 200 -sessions 2000 > loadtest/sessions.json
//
// Each signed-in k6 VU needs a session of its own, and a few hundred accounts
// can hold them all: session i belongs to account i mod -accounts. The
// accounts have the numbers +8490998xxxx, so -accounts is at most 10000.
// k6 rotates the refresh tokens it is given, so each run needs new sessions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
	"uuid"

	"golang.org/x/sync/errgroup"

	"github.com/shopnexus/server/internal/account/devauth"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/postgres"
)

// maxAccounts is how many numbers the +8490998xxxx range holds.
const maxAccounts = 10_000

// session is one entry of the output, the fields k6 needs.
type session struct {
	AccountID    string `json:"account_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"access_token_expires_at"`
}

func main() {
	accounts := flag.Int("accounts", 200, "accounts to sign in")
	sessions := flag.Int("sessions", 1000, "sessions to open across them")
	flag.Parse()
	if err := run(context.Background(), *accounts, *sessions); err != nil {
		fmt.Fprintln(os.Stderr, "loadsessions:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, accounts, sessions int) error {
	if accounts < 1 || accounts > maxAccounts {
		return fmt.Errorf("-accounts %d: want 1 to %d", accounts, maxAccounts)
	}
	if sessions < 1 {
		return fmt.Errorf("-sessions %d: want 1 or more", sessions)
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

	out := make([]session, sessions)
	signIn := func(ctx context.Context, i int) error {
		phone := fmt.Sprintf("+8490998%04d", i%accounts)
		signed, err := svc.SignIn(ctx, service.SignInInput{
			IdempotencyKey: uuid.NewV7().String(), Proof: service.Proof{PhoneToken: phone}, DeviceName: "loadtest",
		})
		if err != nil {
			return fmt.Errorf("sign in %s: %w", phone, err)
		}
		out[i] = session{
			AccountID: signed.Account.ID.String(), AccessToken: signed.AccessToken,
			RefreshToken: signed.RefreshToken, ExpiresAt: signed.AccessTokenExpiresAt.Format(time.RFC3339),
		}
		return nil
	}
	// The first wave signs each account up once; two sign-ups of one number
	// at the same time would race.
	first := min(accounts, sessions)
	if err := parallel(ctx, 0, first, signIn); err != nil {
		return err
	}
	if err := parallel(ctx, first, sessions, signIn); err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil { //nolint:gosec // printing the tokens is the command's job
		return fmt.Errorf("print sessions: %w", err)
	}
	return nil
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
