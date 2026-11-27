// Package agent serves the console agent in the server: the console's
// agent endpoints, whose turns run in the agent runtime and stream back,
// and the gate through which a turn's agent calls the console API with its
// token.
package agent

import (
	"context"
	"net/http"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/handler"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/agent/worker"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/provider/bifrost"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

const (
	// sweepEvery is how often reads and calls past service.ReadsKeptFor
	// are deleted and command outputs as old blanked.
	sweepEvery = time.Hour
	// turnSweepEvery is how often turns whose run was lost are closed, and
	// how often Run sends the jobs.
	turnSweepEvery = time.Minute
	// missGap is the least time between two reconciles a missed key asks for.
	missGap = time.Minute
)

// periodic are the agent's jobs, each with its period.
var periodic = []struct {
	name  string
	every time.Duration
}{
	{worker.JobTurns, turnSweepEvery},
	{worker.JobReads, sweepEvery},
	{worker.JobGateway, service.ReconcileEvery},
}

// Settings is what agent's Mount takes beyond module.Deps.
type Settings struct {
	Config config.AgentConfig
	// TextMin and PhotoMin are the similarity floors of the listing page's
	// price guide, EMBED_TEXT_MIN_SIMILARITY and EMBED_PHOTO_MIN_SIMILARITY.
	TextMin, PhotoMin float64
	// Gateway is Bifrost's admin API, and Ring the server's keys, which
	// the reconciler fills.
	Gateway bifrost.Admin
	Ring    *bifrost.Ring
	// Doc is the API spec: its x-capability says which operations an
	// agent may be given.
	Doc *openapi3.T
}

// Mounted is the agent module as app runs it: its Service, its Restate
// services, its gate, and Run, which follows turns and sends the periodic
// jobs until app stops.
type Mounted struct {
	Service  service.Service
	Services []restate.ServiceDefinition
	handler  *handler.Handler
	d        module.Deps
	// missed is a reconcile the ring asked for, when it lacked a caller.
	missed chan struct{}
}

// Mount registers the console's agent endpoints behind moderator, console's
// guard of a moderator's route, and the AI settings behind admin, over the
// services of console, support, account, catalog, deal, shipping and trust
// that the record pages read. Its worker blanks the command outputs about
// a deleted account.
func Mount(d module.Deps, s Settings, moderator, admin func(http.Handler) http.Handler, consoles consolesvc.Service,
	support supportsvc.Service, accounts accountsvc.Service, catalog catalogsvc.Service, deals dealsvc.Service,
	shipping shippingsvc.Service, trust trustsvc.Service,
) (*Mounted, error) {
	ops, err := domain.NewCatalog(s.Doc)
	if err != nil {
		return nil, err
	}
	svc := service.New(service.Deps{
		Reads: repository.NewReads(d.Pool), Settings: repository.NewSettings(d.Pool),
		Conversations: repository.NewConversations(d.Pool, d.Log), Ops: ops,
		Gateway: s.Gateway, Ring: s.Ring,
		Console: consoles, Accounts: accounts, Catalog: catalog, Deals: deals, Shipping: shipping, Support: support, Trust: trust,
		TextMin: s.TextMin, PhotoMin: s.PhotoMin,
		RuntimeURL: s.Config.RuntimeURL, RuntimeKey: s.Config.RuntimeKey, HTTP: httpx.NewClient(0), Now: d.Now,
		Log: d.Log,
	})
	h := handler.New(svc, d.Log)
	h.Register(d.Router, moderator, admin)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Agent: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	jobs := worker.Jobs{Prefix: d.ServicePrefix, Agent: svc, Log: d.Log}
	missed := make(chan struct{}, 1)
	if s.Ring != nil {
		s.Ring.OnMiss(func(string) {
			select {
			case missed <- struct{}{}:
			default:
			}
		})
	}
	return &Mounted{
		Service: svc, Services: []restate.ServiceDefinition{restate.Reflect(watch), restate.Reflect(jobs)}, handler: h, d: d, missed: missed,
	}, nil
}

// Gate lets an agent's turn call the console API with its token; route
// names the route pattern that serves a method and path. It goes outside
// the request validator.
func (m *Mounted) Gate(route func(method, path string) string) func(http.Handler) http.Handler {
	return m.handler.Gate(route)
}

// SetRoot hands the module the server's whole handler, which an approved
// change goes through as the approver's request.
func (m *Mounted) SetRoot(root http.Handler) { m.handler.SetRoot(root) }

// Run hands the turns' event streams their rings and sends the periodic
// jobs every turnSweepEvery, and a reconcile when the ring lacks a caller's
// key (at most every missGap); until ctx is done, then it stops the runs
// this replica follows and returns nil. The first send waits a tick, so
// this version is registered by then and a deploy's first jobs run on it.
func (m *Mounted) Run(ctx context.Context) error {
	followed := make(chan struct{})
	go func() {
		defer close(followed)
		m.Service.FollowTurns(ctx)
	}()
	defer func() { <-followed }()
	tick := time.NewTicker(turnSweepEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			m.Service.StopRuns()
			return nil
		case <-tick.C:
			for _, j := range periodic {
				m.send(ctx, j.name, "every", j.every)
			}
		case <-m.missed:
			m.send(ctx, worker.JobGateway, "miss", missGap)
		}
	}
}

// send asks Restate to run job once in the period every that now falls in.
func (m *Mounted) send(ctx context.Context, job, why string, every time.Duration) {
	key := worker.JobKey(job, why, m.d.Now(), every)
	if _, err := ingress.ServiceSend[string](m.d.Ingress, m.d.ServicePrefix+worker.JobsService, "Run").
		Send(ctx, job, restate.WithIdempotencyKey(key)); err != nil && ctx.Err() == nil {
		m.d.Log.WarnContext(ctx, "agent: send a job", "job", job, "err", err)
	}
}
