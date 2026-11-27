// Package authn carries who is calling through the request context. The auth
// module's middleware fills it; business modules read it without importing
// auth or knowing how tokens work.
package authn

import (
	"context"
	"net/http"
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// ErrUnauthenticated answers a request that needs a signed-in caller and has
// none.
var ErrUnauthenticated = apperr.New(apperr.KindUnauthenticated, "unauthenticated", "sign in first")

// Principal is the signed-in caller.
type Principal struct {
	AccountID uuid.UUID
	SessionID uuid.UUID
	AuthTime  time.Time // when the person last proved who they are
	// ExpiresAt is when the access token stops being valid; a WebSocket
	// closes then unless the app sends a newer token.
	ExpiresAt time.Time
}

type key struct{}

// With returns ctx carrying p.
func With(ctx context.Context, p Principal) context.Context {
	if s, ok := ctx.Value(seenKey{}).(*Seen); ok {
		s.Account = p.AccountID
	}
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
	// Role is the member's role as module console names it, read with the
	// session on this request.
	Role string
	IP   netip.Addr
}

type consoleMemberKey struct{}

// WithConsoleMember returns ctx carrying s.
func WithConsoleMember(ctx context.Context, s ConsoleMember) context.Context {
	if seen, ok := ctx.Value(seenKey{}).(*Seen); ok {
		seen.Member = s.MemberID
	}
	return context.WithValue(ctx, consoleMemberKey{}, s)
}

// ConsoleMemberFrom returns the console caller in ctx, if any.
func ConsoleMemberFrom(ctx context.Context) (ConsoleMember, bool) {
	s, ok := ctx.Value(consoleMemberKey{}).(ConsoleMember)
	return s, ok
}

// Turn is a console agent's turn calling the console API with its token,
// for the member it works for. The agent module's gate puts it in the
// request; the console's guard then lets the call through as that member,
// within the member's role, and hands a change to Hold instead of the
// handler, as a change waits for a member's approval.
type Turn struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	MemberID       uuid.UUID
	// Capabilities are what the turn's agent holds.
	Capabilities []string
	// Role is the member's console role when the turn's run started.
	Role string
	// Hold answers a change the agent asked for with the approval it now
	// waits on.
	Hold func(w http.ResponseWriter, r *http.Request)
}

type turnKey struct{}

// WithTurn returns ctx carrying t.
func WithTurn(ctx context.Context, t Turn) context.Context {
	return context.WithValue(ctx, turnKey{}, t)
}

// TurnFrom returns the agent's turn in ctx, if any.
func TurnFrom(ctx context.Context) (Turn, bool) {
	t, ok := ctx.Value(turnKey{}).(Turn)
	return t, ok
}

// Seen is who a request turned out to be. A middleware outside the auth
// middleware reads it after the handler returns, e.g. for the access log.
type Seen struct {
	Account uuid.UUID // set by With
	Member  uuid.UUID // set by WithConsoleMember
}

type seenKey struct{}

// Track returns ctx with an empty Seen that With and WithConsoleMember fill
// in on any context derived from it.
func Track(ctx context.Context) (context.Context, *Seen) {
	s := &Seen{}
	return context.WithValue(ctx, seenKey{}, s), s
}
