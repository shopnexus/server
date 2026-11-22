// Package agent serves the console agent's two sides in the server: the
// console's agent endpoints, whose runs the agent service streams back,
// and the MCP server the agent reads through and polls a run's control
// on, on a listener of its own.
package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/agent/handler"
	"github.com/shopnexus/server/internal/agent/mcp"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/agent/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/provider/litellm"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

const (
	// sweepEvery is how often reads past service.ReadsKeptFor are deleted,
	// as the events are.
	sweepEvery = time.Hour
	// stopWait bounds the MCP server's shutdown.
	stopWait = 10 * time.Second
	// turnSweepEvery is how often turns stuck running are closed.
	turnSweepEvery = time.Minute
)

// Settings is what agent's Mount takes beyond module.Deps.
type Settings struct {
	Config config.AgentConfig
	// TextMin and PhotoMin are the similarity floors price_guide samples
	// with, EMBED_TEXT_MIN_SIMILARITY and EMBED_PHOTO_MIN_SIMILARITY.
	TextMin, PhotoMin float64
}

// Mounted is the agent module as app runs it: its Service, and Run, which
// serves the MCP server until app stops.
type Mounted struct {
	Service  service.Service
	listener net.Listener
	mcp      http.Handler
	d        module.Deps
}

// Mount registers the console's agent endpoints behind moderator, console's
// guard of a moderator's route, and the AI settings behind admin, builds the
// MCP server over the services of console, support, account, catalog,
// deal, shipping and trust, and opens its listener at AGENT_MCP_ADDR, so an
// address in use stops the start.
func Mount(d module.Deps, s Settings, moderator, admin func(http.Handler) http.Handler, consoles consolesvc.Service,
	support supportsvc.Service, accounts accountsvc.Service, catalog catalogsvc.Service, deals dealsvc.Service,
	shipping shippingsvc.Service, trust trustsvc.Service,
) (*Mounted, error) {
	svc := service.New(service.Deps{
		Reads: repository.NewReads(d.Pool), Settings: repository.NewSettings(d.Pool), Runs: repository.NewRuns(d.Pool, d.Log),
		Gateway: litellm.Admin{BaseURL: s.Config.LiteLLMURL, MasterKey: s.Config.LiteLLMMasterKey, HTTP: httpx.NewClient(0)},
		Console: consoles, Accounts: accounts, Catalog: catalog, Deals: deals, Shipping: shipping, Support: support, Trust: trust,
		TextMin: s.TextMin, PhotoMin: s.PhotoMin,
		BaseURL: s.Config.BaseURL, Token: s.Config.ServiceToken, HTTP: httpx.NewClient(0), Now: d.Now,
		Log: d.Log,
	})
	handler.New(svc, d.Log).Register(d.Router, moderator, admin)
	h := mcp.Handler(mcp.Deps{
		Token: s.Config.MCPToken, Console: consoles, Agent: svc,
		Support: support, Accounts: accounts, Catalog: catalog, Deals: deals, Shipping: shipping, Trust: trust,
		TextMin: s.TextMin, PhotoMin: s.PhotoMin, Now: d.Now, Log: d.Log,
	})
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", s.Config.MCPAddr)
	if err != nil {
		return nil, fmt.Errorf("listen for MCP at AGENT_MCP_ADDR: %w", err)
	}
	return &Mounted{Service: svc, listener: l, mcp: httpx.Trace(d.Log)(httpx.Recover(d.Log)(h)), d: d}, nil
}

// Addr is where the MCP server listens, for a test that listens on port 0.
func (m *Mounted) Addr() net.Addr { return m.listener.Addr() }

// Run serves the MCP server, hands the turns' event streams their rings,
// sweeps old reads at start and every hour, closes stuck turns at start
// and every minute, and syncs the models of the providers in sync mode at
// start and every 6 hours, until ctx is done; then it waits for the calls
// in flight and returns nil.
func (m *Mounted) Run(ctx context.Context) error {
	srv := &http.Server{Handler: m.mcp, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(m.listener) }()
	followed := make(chan struct{})
	go func() {
		defer close(followed)
		m.Service.FollowTurns(ctx)
	}()
	defer func() { <-followed }()
	sweepTurns := func() {
		if err := m.Service.SweepTurns(ctx); err != nil && ctx.Err() == nil {
			m.d.Log.WarnContext(ctx, "agent: sweep stuck turns", "err", err)
		}
	}
	sweep := func() {
		if err := m.Service.SweepReads(ctx); err != nil && ctx.Err() == nil {
			m.d.Log.WarnContext(ctx, "agent: sweep reads", "err", err)
		}
	}
	sync := func() {
		if err := m.Service.SyncProviders(ctx); err != nil && ctx.Err() == nil {
			m.d.Log.WarnContext(ctx, "agent: sync the providers' models", "err", err)
		}
	}
	sweep()
	sweepTurns()
	sync()
	tick := time.NewTicker(sweepEvery)
	defer tick.Stop()
	turnTick := time.NewTicker(turnSweepEvery)
	defer turnTick.Stop()
	syncTick := time.NewTicker(service.SyncEvery)
	defer syncTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return shutdown(ctx, srv)
		case err := <-served:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return fmt.Errorf("serve MCP: %w", err)
		case <-tick.C:
			sweep()
		case <-turnTick.C:
			sweepTurns()
		case <-syncTick.C:
			sync()
		}
	}
}

// shutdown stops srv once ctx is done, waiting stopWait at most for the
// calls in flight.
func shutdown(ctx context.Context, srv *http.Server) error {
	stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopWait)
	defer cancel()
	if err := srv.Shutdown(stop); err != nil {
		return fmt.Errorf("stop the MCP server: %w", err)
	}
	return nil
}
