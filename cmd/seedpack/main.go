// Command seedpack builds, checks, loads and wipes seed packs (see
// internal/seedpack) in a dev stack: go run ./cmd/seedpack --help.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"

	"github.com/restatedev/sdk-go/ingress"
	"github.com/spf13/cobra"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/seedpack"
	"github.com/shopnexus/server/internal/seedpack/fixtures"
	"github.com/shopnexus/server/internal/seedpack/legacy"
	"github.com/shopnexus/server/internal/seedpack/loader"
)

// builders are the sources `seedpack build` knows. A new source is a
// seedpack.Builder in its own file or package, added here.
func builders() []seedpack.Builder {
	return []seedpack.Builder{
		&fixtures.Builder{},
		&legacy.Builder{},
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := root().ExecuteContext(ctx)
	stop()
	if errors.Is(err, errCancelled) {
		fmt.Fprintln(os.Stderr, "cancelled; nothing written")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "seedpack:", err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seedpack",
		Short: "Build, check and load seed packs into a dev stack",
		Long: `seedpack moves a marketplace in and out of a dev stack. A seed pack is a
directory of JSONL, JPEG and .npy files (internal/seedpack documents it):
people, listings, photos, sold items with their reviews, follows and search
vectors. Builders make a pack once from a source; load writes it through the
server's own rules as many times as you like, and skips what is there.

load, wipe and embed read the server's .env and refuse any APP_ENV but dev.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddGroup(&cobra.Group{ID: "stack", Title: "Use a pack in a stack:"}, &cobra.Group{ID: "make", Title: "Make a pack:"})
	for _, c := range []*cobra.Command{loadCmd(), wipeCmd()} {
		c.GroupID = "stack"
		cmd.AddCommand(c)
	}
	for _, c := range []*cobra.Command{buildCmd(), embedCmd(), verifyCmd()} {
		c.GroupID = "make"
		cmd.AddCommand(c)
	}
	cmd.SetHelpCommandGroupID("make")
	cmd.SetCompletionCommandGroupID("make")
	return cmd
}

// stack is the dev stack a command writes to.
type stack struct {
	cfg   config.Config
	pool  *pgxpool.Pool
	store *objectstore.Store
}

// openStack reads .env, refuses anything but APP_ENV=dev and connects.
func openStack(ctx context.Context) (*stack, error) {
	cfg, err := config.FromDotEnv(".env")
	if err != nil {
		return nil, err
	}
	if cfg.Env != config.EnvDev {
		return nil, fmt.Errorf("APP_ENV is %s; seedpack only writes to a dev stack", cfg.Env)
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	store, err := objectstore.New(cfg.ObjectStore)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to the object store: %w", err)
	}
	return &stack{cfg: cfg, pool: pool, store: store}, nil
}

func (s *stack) close() { s.pool.Close() }

// deps are the loader's view of the stack. A kind of vector that is off in
// the stack has no version.
func (s *stack) deps() loader.Deps {
	d := loader.Deps{
		Pool: s.pool, Store: s.store, Ingress: ingress.NewClient(s.cfg.Restate.IngressURL), Prefix: s.cfg.Restate.ServicePrefix,
	}
	if s.cfg.Embed.TextOn() {
		d.TextVersion = s.cfg.Embed.TextVersion
	}
	if s.cfg.AI.EmbedOn() {
		d.ImageVersion = s.cfg.Embed.ImageVersion
	}
	return d
}

// target names the database and the bucket, without credentials.
func (s *stack) target() string {
	db := "postgres"
	if u, err := url.Parse(s.cfg.DatabaseURL); err == nil {
		db = u.Host + u.Path
	}
	return fmt.Sprintf("%s · %s/%s · APP_ENV=%s", db, s.cfg.ObjectStore.Endpoint, s.cfg.ObjectStore.Bucket, s.cfg.Env)
}

// packArg is the pack a command names, by --pack or its first argument.
func packArg(flag string, args []string) (string, error) {
	switch {
	case flag != "" && len(args) > 0:
		return "", errors.New("name the pack once, by --pack or as an argument")
	case flag != "":
		return flag, nil
	case len(args) == 1:
		return args[0], nil
	}
	return "", errors.New("which pack? pass a directory, a URL or a name such as demo")
}
