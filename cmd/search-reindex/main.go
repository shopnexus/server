// Command search-reindex asks ListingEmbed to refresh every listing that
// should have vectors: the backfill of listings posted before search had
// vectors, and the way to move every listing to a new EMBED_*_VERSION. Each
// send queues behind a listing's run in progress, and a listing already in
// line finds nothing to do, so running it twice is harmless.
//
//	go run ./cmd/search-reindex -every 2s
//
// -every spreads the refreshes: listing i starts i × every from now, so a
// backfill does not start every listing at once. The command returns once
// every send is accepted.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("search-reindex", flag.ContinueOnError)
	every := flags.Duration("every", 0, "the time between two listings' refreshes, such as 2s; required")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("-every: %w", err)
	}
	if *every <= 0 {
		return errors.New("-every is required: the time between two listings' refreshes, such as 2s")
	}
	cfg, err := config.FromDotEnv(".env")
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()
	ids, err := repository.NewVectors(pool).Embeddable(ctx)
	if err != nil {
		return fmt.Errorf("list the listings to embed: %w", err)
	}
	client := ingress.NewClient(cfg.Restate.IngressURL)
	name := cfg.Restate.ServicePrefix + service.ListingEmbedName
	for i, id := range ids {
		if _, err = ingress.ObjectSend[restate.Void](client, name, id.String(), "Refresh").Send(ctx, restate.Void{}, restate.WithDelay(time.Duration(i)**every)); err != nil {
			return fmt.Errorf("send %s after %d of %d: %w", id, i, len(ids), err)
		}
	}
	fmt.Printf("sent %d listings to %s, one every %s\n", len(ids), name, *every)
	return nil
}
