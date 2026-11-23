package worker

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/discovery/service"
)

// SearchVectorSweep sweeps the cached text vectors once; the search that
// first writes one on a day sends it, keyed by the day.
type SearchVectorSweep struct {
	Prefix    string
	Discovery service.Service
}

func (w SearchVectorSweep) ServiceName() string { return w.Prefix + service.VectorSweepService }

func (w SearchVectorSweep) Run(ctx restate.Context, _ string) error {
	if _, err := restate.Run(ctx, func(rc restate.RunContext) (int64, error) {
		return w.Discovery.SweepVectors(rc)
	}); err != nil {
		return fmt.Errorf("sweep the text vectors: %w", err)
	}
	return nil
}
