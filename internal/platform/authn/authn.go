// Package authn carries who is calling through the request context. The auth
// module's middleware fills it; business modules read it without importing
// auth or knowing how tokens work.
package authn

import (
	"context"
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// ErrUnauthenticated answers a request that needs a signed-in caller and has
// none.
var ErrUnauthenticated = apperr.New(apperr.KindUnauthenticated, "unauthenticated", "sign in first")

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

// ConsoleMember is the signed-in console member. It is a different type from
// Principal, so a customer handler never receives a console caller or the
// other way round.
type ConsoleMember struct {
	MemberID  uuid.UUID
	SessionID uuid.UUID
	IP        netip.Addr
}

type consoleMemberKey struct{}

// WithConsoleMember returns ctx carrying s.
func WithConsoleMember(ctx context.Context, s ConsoleMember) context.Context {
	return context.WithValue(ctx, consoleMemberKey{}, s)
}

// ConsoleMemberFrom returns the console caller in ctx, if any.
func ConsoleMemberFrom(ctx context.Context) (ConsoleMember, bool) {
	s, ok := ctx.Value(consoleMemberKey{}).(ConsoleMember)
	return s, ok
}
