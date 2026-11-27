package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/agent/service"
)

// JobsService is the Restate service of the agent's periodic jobs, without
// the prefix, and these the jobs its Run takes.
const (
	JobsService = "AgentJobs"
	JobTurns    = "turns"   // close the turns whose run was lost
	JobReads    = "reads"   // delete old reads and calls, blank old outputs
	JobGateway  = "gateway" // reconcile Bifrost with the AI settings
)

// Jobs runs the agent's periodic jobs. Every replica sends each one keyed
// by its period, so Restate runs it once and on the newest deployment: a
// version left draining after a deploy never runs one on a newer schema.
type Jobs struct {
	Prefix string
	Agent  service.Service
	Log    *slog.Logger
}

func (w Jobs) ServiceName() string { return w.Prefix + JobsService }

// JobKey is the idempotency key of job's send at at: it names the period
// every that at falls in, so the replicas' sends in one period run it once.
// why tells a scheduled send from one asked for on the side.
func JobKey(job, why string, at time.Time, every time.Duration) string {
	return fmt.Sprintf("%s-%s-%d", job, why, at.UTC().Truncate(every).Unix())
}

// Run runs job once. A failed job is logged and waits for the next period,
// so a store that is down leaves no invocations retrying behind it.
func (w Jobs) Run(ctx restate.Context, job string) error {
	var run func(context.Context) error
	switch job {
	case JobTurns:
		run = w.Agent.SweepTurns
	case JobReads:
		run = w.Agent.SweepReads
	case JobGateway:
		run = w.Agent.Reconcile
	default:
		//nolint:wrapcheck // Restate must receive the terminal error itself
		return restate.TerminalErrorf("no agent job %q", job)
	}
	// The error is logged inside the step: Bifrost's answers stay out of
	// the journal.
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		if stepErr := run(rc); stepErr != nil {
			w.Log.WarnContext(rc, "agent job", "job", job, "err", stepErr)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("agent job %s: %w", job, err)
	}
	return nil
}
