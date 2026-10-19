// Package authn carries who is calling through the request context. The auth
// module's middleware fills it; business modules read it without importing
// auth or knowing how tokens work.
package authn

import (
	"context"
	"time"
	"uuid"
)

// Principal is the signed-in caller.
type Principal struct {
	AccountID uuid.UUID
	SessionID uuid.UUID
	AuthTime  time.Time // when the person last proved who they are
}

type key struct{}

// With returns ctx carrying p.
func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, key{}, p)
}

// From returns the principal in ctx, if any.
func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(key{}).(Principal)
	return p, ok
}
