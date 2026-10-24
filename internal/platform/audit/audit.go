// Package audit records what staff, the CLI and anonymous sign-in attempts
// did. An adapter writes an Entry with Insert inside the transaction of the
// change it records, so the change and its record commit together or not at
// all. Diff takes view structs, never entities, so no secret reaches a row.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/authn"
)

type ActorKind string

const (
	ActorStaff     ActorKind = "staff"
	ActorCLI       ActorKind = "cli"
	ActorAnonymous ActorKind = "anonymous"
)

// Actor is who acted. StaffID is set for ActorStaff, SessionID when a staff
// session acted, Name for the CLI, and IP for everything but the CLI.
type Actor struct {
	Kind      ActorKind
	StaffID   uuid.UUID
	SessionID uuid.UUID
	Name      string
	IP        netip.Addr
}

// ByStaff is the signed-in member the middleware put in the context.
func ByStaff(s authn.Staff) Actor {
	return Actor{Kind: ActorStaff, StaffID: s.StaffID, SessionID: s.SessionID, Name: "", IP: s.IP}
}

// BySignIn is a member acting during sign-in, before a session exists.
func BySignIn(staffID uuid.UUID, ip netip.Addr) Actor {
	return Actor{Kind: ActorStaff, StaffID: staffID, SessionID: uuid.UUID{}, Name: "", IP: ip}
}

// ByCLI is cmd/staff run by osUser.
func ByCLI(osUser string) Actor {
	return Actor{Kind: ActorCLI, StaffID: uuid.UUID{}, SessionID: uuid.UUID{}, Name: osUser, IP: netip.Addr{}}
}

// Anonymous is a sign-in attempt that proved nothing.
func Anonymous(ip netip.Addr) Actor {
	return Actor{Kind: ActorAnonymous, StaffID: uuid.UUID{}, SessionID: uuid.UUID{}, Name: "", IP: ip}
}

// Target is what an action changed; the zero Target is none.
type Target struct {
	Kind string
	ID   uuid.UUID
}

func Account(id uuid.UUID) Target { return Target{Kind: "account", ID: id} }
func Staff(id uuid.UUID) Target   { return Target{Kind: "staff", ID: id} }

// Entry is one row of staff_actions.
type Entry struct {
	Actor  Actor
	Action string // "account.suspend", "staff.disable", ...
	Target Target
	Diff   map[string]any // changed fields as [before, after]
	At     time.Time
}

// Diff returns the fields whose JSON differs between before and after, each
// as [before, after]. before is nil for a creation. Both must marshal to a
// JSON object; anything else is a mistake in the caller and panics.
func Diff(before, after any) map[string]any {
	b, a := fields(before), fields(after)
	out := map[string]any{}
	for k, av := range a {
		if bv, ok := b[k]; !ok || !reflect.DeepEqual(bv, av) {
			out[k] = []any{b[k], av}
		}
	}
	for k, bv := range b {
		if _, ok := a[k]; !ok {
			out[k] = []any{bv, nil}
		}
	}
	return out
}

func fields(v any) map[string]any {
	m := map[string]any{}
	if v == nil {
		return m
	}
	raw, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err != nil {
		panic(fmt.Sprintf("audit: %T is not a JSON object: %v", v, err))
	}
	return m
}

const insertAction = `
INSERT INTO staff_actions (id, actor_kind, staff_id, actor_name, session_id, ip, action, target_kind, target_id, diff, at)
VALUES (@id, @actor_kind, @staff_id, @actor_name, @session_id, @ip, @action, @target_kind, @target_id, @diff, @at)`

// Insert writes e in tx, the transaction of the change it records.
func Insert(ctx context.Context, tx pgx.Tx, e Entry) error {
	diff := e.Diff
	if diff == nil {
		diff = map[string]any{}
	}
	_, err := tx.Exec(ctx, insertAction, pgx.StrictNamedArgs{
		"id": uuid.NewV7(), "actor_kind": e.Actor.Kind, "staff_id": nullID(e.Actor.StaffID),
		"actor_name": nullText(e.Actor.Name), "session_id": nullID(e.Actor.SessionID), "ip": nullIP(e.Actor.IP),
		"action": e.Action, "target_kind": nullText(e.Target.Kind), "target_id": nullID(e.Target.ID),
		"diff": diff, "at": e.At,
	})
	if err != nil {
		return fmt.Errorf("insert staff action: %w", err)
	}
	return nil
}

func nullID(id uuid.UUID) any {
	if id == (uuid.UUID{}) {
		return nil
	}
	return id
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIP(ip netip.Addr) any {
	if !ip.IsValid() {
		return nil
	}
	return ip
}
