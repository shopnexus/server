// Package audit records what console members and the CLI did to accounts
// and to console members. An adapter writes an Entry with Insert inside the transaction of
// the change it records, so the change and its record commit together or
// not at all.
package audit

import (
	"context"
	"fmt"
	"net/netip"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/authn"
)

type ActorKind string

const (
	ActorMember ActorKind = "member"
	ActorCLI    ActorKind = "cli"
)

// Actor is who acted: a console member through a session, with the IP of the
// request, or the CLI run by the OS user Name.
type Actor struct {
	Kind      ActorKind
	MemberID  uuid.UUID
	SessionID uuid.UUID
	Name      string
	IP        netip.Addr
}

// ByMember is the signed-in member the middleware put in the context.
func ByMember(s authn.ConsoleMember) Actor {
	return Actor{Kind: ActorMember, MemberID: s.MemberID, SessionID: s.SessionID, IP: s.IP}
}

// ByCLI is cmd/console run by osUser.
func ByCLI(osUser string) Actor { return Actor{Kind: ActorCLI, Name: osUser} }

// ActionKind is what was done: the action of the Action schema.
type ActionKind string

const (
	AccountSuspend      ActionKind = "account.suspend"
	AccountUnsuspend    ActionKind = "account.unsuspend"
	MemberCreate        ActionKind = "member.create"
	MemberDisable       ActionKind = "member.disable"
	MemberEnable        ActionKind = "member.enable"
	MemberResetPassword ActionKind = "member.reset_password"
	MemberResetTOTP     ActionKind = "member.reset_totp"
	MemberUnlock        ActionKind = "member.unlock"
)

type TargetKind string

const (
	TargetAccount TargetKind = "account"
	TargetMember  TargetKind = "member"
)

// Target is what an action changed.
type Target struct {
	Kind TargetKind
	ID   uuid.UUID
}

func Account(id uuid.UUID) Target { return Target{Kind: TargetAccount, ID: id} }
func Member(id uuid.UUID) Target  { return Target{Kind: TargetMember, ID: id} }

// Entry is one row of console_actions. Details holds what the action kind
// does not say, such as a suspension's reason; never a hash or a secret.
type Entry struct {
	Actor   Actor
	Action  ActionKind
	Target  Target
	Details map[string]any
	At      time.Time
}

const insertAction = `
INSERT INTO console_actions (id, actor_kind, member_id, actor_name, session_id, ip, action_kind, target_kind, target_id, details, acted_at)
VALUES (@id, @actor_kind, @member_id, @actor_name, @session_id, @ip, @action_kind, @target_kind, @target_id, @details, @acted_at)`

// Insert writes e in tx, the transaction of the change it records.
func Insert(ctx context.Context, tx pgx.Tx, e Entry) error {
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	_, err := tx.Exec(ctx, insertAction, pgx.StrictNamedArgs{
		"id": uuid.NewV7(), "actor_kind": e.Actor.Kind, "member_id": nullID(e.Actor.MemberID),
		"actor_name": nullText(e.Actor.Name), "session_id": nullID(e.Actor.SessionID), "ip": nullIP(e.Actor.IP),
		"action_kind": e.Action, "target_kind": e.Target.Kind, "target_id": e.Target.ID, "details": details, "acted_at": e.At,
	})
	if err != nil {
		return fmt.Errorf("insert console action: %w", err)
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
