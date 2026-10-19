// Package app builds every dependency of the server by hand. cmd/server and
// the e2e tests both start the server through New.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/jobs"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
	"github.com/shopnexus/server2/migrations"
)

// migrationFS is the schema New checks against; tests replace it.
var migrationFS fs.FS = migrations.FS

// App is the assembled server.
type App struct {
	Handler http.Handler
	router  *httpx.Router
	pool    *pgxpool.Pool
	nc      *nats.Conn

	// Background work (relay, DLQ, consumers) stops when stop is called.
	stop      context.CancelFunc
	wg        sync.WaitGroup
	consumers []func()
}

// Routes lists every registered route, for the route-versus-spec test.
func (a *App) Routes() []httpx.Route { return a.router.Routes() }

// New builds the server for cfg and starts its background work. It refuses to
// start while migrations are pending, so the server never runs against an
// older schema.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	a := &App{pool: pool}

	pending, err := postgres.Pending(ctx, pool, migrationFS)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if len(pending) > 0 {
		pool.Close()
		names := make([]string, len(pending))
		for i, m := range pending {
			names[i] = m.File()
		}
		return nil, fmt.Errorf("migrations pending: %s; run make migrate", strings.Join(names, ", "))
	}

	if err := a.startJobs(ctx, cfg, log); err != nil {
		a.Close(context.Background())
		return nil, err
	}

	doc, err := api.Load()
	if err != nil {
		a.Close(context.Background())
		return nil, fmt.Errorf("load api spec: %w", err)
	}
	validate, err := httpx.ValidateRequests(doc, log)
	if err != nil {
		a.Close(context.Background())
		return nil, err
	}

	a.router = httpx.NewRouter()
	a.router.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.Respond(w, r, http.StatusOK, status{"ok"})
	})
	a.router.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.ready(r.Context()); err != nil {
			log.WarnContext(r.Context(), "not ready", "err", err)
			httpx.Respond(w, r, http.StatusServiceUnavailable, status{"unavailable"})
			return
		}
		httpx.Respond(w, r, http.StatusOK, status{"ok"})
	})

	// Trace is outermost so a panic's 500 still carries a trace id.
	a.Handler = httpx.Trace(httpx.Recover(log)(httpx.AccessLog(log)(validate(a.router))))
	return a, nil
}

type status struct {
	Status string `json:"status"`
}

// startJobs connects to NATS, creates the streams, and starts the relay, the
// dead-letter copier and one consumer per command.
func (a *App) startJobs(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	nc, err := connectNATS(ctx, cfg.NATSURL, log)
	if err != nil {
		return err
	}
	a.nc = nc
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	if err := jobs.EnsureStreams(ctx, js); err != nil {
		return fmt.Errorf("jetstream streams: %w", err)
	}

	bg, stop := context.WithCancel(context.Background())
	a.stop = stop
	relay := jobs.NewRelay(a.pool, js, log)
	a.wg.Go(func() { relay.Run(bg) })
	a.wg.Go(func() { jobs.RunDLQ(bg, nc, js, log) })

	smsSender := sms.LogSender{Log: log}
	stopSMS, err := jobs.Handle(ctx, js, log, jobs.HandleOptions{Durable: "sms-sender"},
		func(ctx context.Context, c sms.SendCommand) error { return smsSender.Send(ctx, c.To, c.Text) })
	if err != nil {
		return err
	}
	a.consumers = append(a.consumers, stopSMS)

	emailSender := email.LogSender{Log: log}
	stopEmail, err := jobs.Handle(ctx, js, log, jobs.HandleOptions{Durable: "email-sender"},
		func(ctx context.Context, c email.SendCommand) error {
			return emailSender.Send(ctx, c.To, c.Title, c.Body)
		})
	if err != nil {
		return err
	}
	a.consumers = append(a.consumers, stopEmail)
	return nil
}

// connectNATS retries for up to 30 s, so the server can start while the
// broker is still coming up.
func connectNATS(ctx context.Context, url string, log *slog.Logger) (*nats.Conn, error) {
	var err error
	for attempt := range 15 {
		var nc *nats.Conn
		nc, err = nats.Connect(url, nats.Name("server2"), nats.MaxReconnects(-1))
		if err == nil {
			return nc, nil
		}
		log.WarnContext(ctx, "nats not reachable yet", "attempt", attempt+1, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("connect to nats: %w", err)
}

func (a *App) ready(ctx context.Context) error {
	if a.nc == nil || a.nc.Status() != nats.CONNECTED {
		return errors.New("nats not connected")
	}
	return a.pool.Ping(ctx)
}

// Close stops background work and releases what New opened.
func (a *App) Close(ctx context.Context) error {
	a.stopBackground()
	if a.nc != nil {
		a.nc.Close()
	}
	a.pool.Close()
	return nil
}

// stopBackground stops consumers, the relay and the DLQ copier and waits for
// them. It runs before the pool closes: the relay holds a connection for its
// lock, and pgxpool.Close waits for every connection to come back.
func (a *App) stopBackground() {
	for _, stop := range a.consumers {
		stop()
	}
	a.consumers = nil
	if a.stop != nil {
		a.stop()
	}
	a.wg.Wait()
}
