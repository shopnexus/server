package restatex

import (
	"context"
	"fmt"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// Refusable is the result of a Restate service, which carries a refusal as
// data: Restate retries an error, and its ingress keeps only the message of
// a terminal one, which would lose the code and the details. The result is a
// named type of its service, because Restate derives a JSON schema ID from
// the type's name and a generic one makes it invalid.
type Refusable interface {
	Refusal() *apperr.Error
}

// Caller calls one handler of a Restate service through the ingress, so a
// use case reads like a plain call.
type Caller[I any, O Refusable] struct {
	Ingress *ingress.Client
	Service string // with the service prefix
	Handler string
}

// Call waits up to CallTimeout and returns the service's refusal as the
// error. A repeated idempotencyKey gets the first call's result.
func (c Caller[I, O]) Call(ctx context.Context, idempotencyKey string, in I) (O, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	res, err := ingress.Service[I, O](c.Ingress, c.Service, c.Handler).
		Request(ctx, in, restate.WithIdempotencyKey(idempotencyKey))
	if err != nil {
		return res, fmt.Errorf("call %s/%s: %w", c.Service, c.Handler, err)
	}
	if refused := res.Refusal(); refused != nil {
		return res, refused
	}
	return res, nil
}
