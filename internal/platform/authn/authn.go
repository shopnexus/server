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

// Staff is the signed-in staff member. It is a different type from
// Principal, so a customer handler never receives a staff caller or the
// other way round. Role is the member's role as staff.Require read it.
type Staff struct {
	StaffID   uuid.UUID
	SessionID uuid.UUID
	Role      string
	IP        netip.Addr
}

type staffKey struct{}

// WithStaff returns ctx carrying s.
func WithStaff(ctx context.Context, s Staff) context.Context {
	return context.WithValue(ctx, staffKey{}, s)
}

// StaffFrom returns the staff caller in ctx, if any.
func StaffFrom(ctx context.Context) (Staff, bool) {
	s, ok := ctx.Value(staffKey{}).(Staff)
	return s, ok
}
