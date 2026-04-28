package orderbiz

import (
	"log/slog"

	restate "github.com/restatedev/sdk-go"
)

// Saga collects compensators in append order and runs them LIFO on failure.
// Each compensator is wrapped in restate.RunVoid → Restate retries idempotently
// on failure. The compensators slice lives in Go memory; on workflow replay it
// is rebuilt deterministically because every action between Defer() calls is
// itself wrapped in restate.Run, so journal replay restores the slice to the
// exact state it had at the crash point.
type Saga struct {
	ctx          restate.WorkflowContext
	compensators []sagaStep
}

type sagaStep struct {
	name string
	fn   func(restate.RunContext) error
}

// NewSaga creates an empty saga bound to the given workflow context.
func NewSaga(ctx restate.WorkflowContext) *Saga {
	return &Saga{ctx: ctx}
}

// Defer appends a compensator. Call BEFORE performing the action it compensates.
func (s *Saga) Defer(name string, fn func(restate.RunContext) error) {
	s.compensators = append(s.compensators, sagaStep{name: name, fn: fn})
}

// Compensate runs all deferred compensators LIFO. Each runs in restate.RunVoid
// so Restate retries indefinitely on failure (compensators must be idempotent).
func (s *Saga) Compensate() {
	for i := len(s.compensators) - 1; i >= 0; i-- {
		step := s.compensators[i]
		if err := restate.RunVoid(s.ctx, func(rctx restate.RunContext) error {
			return step.fn(rctx)
		}, restate.WithName("compensate:"+step.name)); err != nil {
			slog.Error("saga compensate", slog.String("step", step.name), slog.Any("error", err))
		}
	}
}

// Clear drops all pending compensators. Call on the success exit path.
func (s *Saga) Clear() { s.compensators = nil }
