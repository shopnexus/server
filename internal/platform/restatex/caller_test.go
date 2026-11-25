package restatex_test

import (
	"errors"
	"testing"
	"time"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

// gate admits "ok" and refuses anything else, as a service refuses a send.
type gate struct{ prefix string }

type passed struct {
	Value   string        `json:"value"`
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (p passed) Refusal() *apperr.Error { return p.Refused }

func (g gate) ServiceName() string { return g.prefix + "Gate" }

func (gate) Pass(_ restate.Context, in string) (passed, error) {
	if in == "ok" {
		return passed{Value: "passed"}, nil
	}
	refused := apperr.New(apperr.KindRateLimited, "otp_limit", "too many").WithRetryAfter(7 * time.Second)
	return passed{Refused: refused}, nil
}

// The refusal reaches the caller as the *apperr.Error the service returned,
// details included.
func TestCallerReturnsTheRefusal(t *testing.T) {
	t.Parallel()
	g := gate{prefix: restatetest.Prefix()}
	c := restatex.Caller[string, passed]{Ingress: restatetest.Start(t, restate.Reflect(g)), Service: g.ServiceName(), Handler: "Pass"}
	if v, err := c.Call(t.Context(), "ok"); err != nil || v.Value != "passed" {
		t.Fatalf("admitted: %+v %v", v, err)
	}
	_, err := c.Call(t.Context(), "no")
	e, ok := errors.AsType[*apperr.Error](err)
	if !ok || e.Kind != apperr.KindRateLimited || e.Code != "otp_limit" || e.Details.RetryAfter == nil || *e.Details.RetryAfter != 7 {
		t.Fatalf("refused: %#v", err)
	}
}
