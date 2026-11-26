// Package audit records what console members and the CLI did to accounts,
// to console members, to support tickets, to report cases, to listings and
// to feedback.
// A repository queues an Entry with Queue in the batch of the change it
// records, so the change and its record commit together or not at all.
package audit

import (
	"crypto/sha256"
	"net/netip"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/postgres"
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
	MemberRename        ActionKind = "member.rename"
	TicketReply         ActionKind = "ticket.reply"
	TicketResolve       ActionKind = "ticket.resolve"
	TicketReopen        ActionKind = "ticket.reopen"
	ReportDismiss       ActionKind = "report.dismiss"
	ReportResolve       ActionKind = "report.resolve"
	ReportReopen        ActionKind = "report.reopen"
	ListingHide         ActionKind = "listing.hide"
	ListingUnhide       ActionKind = "listing.unhide"
	// AIProviderCreate and the ai.* kinds after it are changes to the AI
	// settings. The provider, rotation and budgets kinds are LiteLLM's,
	// kept for the log's history.
	AIProviderCreate ActionKind = "ai.provider_create"
	AIProviderUpdate ActionKind = "ai.provider_update"
	AIProviderDelete ActionKind = "ai.provider_delete"
	AIProviderModels ActionKind = "ai.provider_models"
	AIModelTags      ActionKind = "ai.model_tags"
	AIModelPrice     ActionKind = "ai.model_price"
	AIAgentCreate    ActionKind = "ai.agent_create"
	AIAgentUpdate    ActionKind = "ai.agent_update"
	AIRotation       ActionKind = "ai.rotation"
	AIBudgets        ActionKind = "ai.budgets"
	// DashboardFloor changes the lowest role that opens ops dashboards.
	DashboardFloor ActionKind = "dashboard.floor"
	// FeedbackStatus and the feedback.* kinds after it are staff's work on
	// feedback: its group's status, the reply, the heart and joining groups.
	FeedbackStatus  ActionKind = "feedback.status"
	FeedbackReply   ActionKind = "feedback.reply"
	FeedbackHeart   ActionKind = "feedback.heart"
	FeedbackUnheart ActionKind = "feedback.unheart"
	FeedbackSameAs  ActionKind = "feedback.same_as"
)

type TargetKind string

const (
	TargetAccount TargetKind = "account"
	TargetMember  TargetKind = "member"
	TargetTicket  TargetKind = "ticket"
	TargetListing TargetKind = "listing"
	// TargetAIProvider and TargetAIModel are named, not numbered: their
	// Target.ID is NamedID of the name, and the entry's details carry it.
	TargetAIProvider TargetKind = "ai_provider"
	TargetAIModel    TargetKind = "ai_model"
	TargetAIAgent    TargetKind = "ai_agent"
	// TargetAISettings is the router's rotation and the budgets, as a whole.
	TargetAISettings TargetKind = "ai_settings"
	// TargetDashboard is an ops dashboard, named by its app.
	TargetDashboard TargetKind = "dashboard"
	TargetFeedback  TargetKind = "feedback"
)

// Target is what an action changed.
type Target struct {
	Kind TargetKind
	ID   uuid.UUID
}

func Account(id uuid.UUID) Target { return Target{Kind: TargetAccount, ID: id} }
func Member(id uuid.UUID) Target  { return Target{Kind: TargetMember, ID: id} }
func Ticket(id uuid.UUID) Target  { return Target{Kind: TargetTicket, ID: id} }
func Listing(id uuid.UUID) Target { return Target{Kind: TargetListing, ID: id} }
func AIAgent(id uuid.UUID) Target { return Target{Kind: TargetAIAgent, ID: id} }

func Feedback(id uuid.UUID) Target { return Target{Kind: TargetFeedback, ID: id} }

// AIProvider is the provider named name.
func AIProvider(name string) Target {
	return Target{Kind: TargetAIProvider, ID: NamedID(TargetAIProvider, name)}
}

// AIModel is the model named name.
func AIModel(name string) Target {
	return Target{Kind: TargetAIModel, ID: NamedID(TargetAIModel, name)}
}

// AISettings is a part of the AI settings as a whole, such as rotation.
func AISettings(name string) Target {
	return Target{Kind: TargetAISettings, ID: NamedID(TargetAISettings, name)}
}

// Dashboard is the ops dashboard of app.
func Dashboard(app string) Target {
	return Target{Kind: TargetDashboard, ID: NamedID(TargetDashboard, app)}
}

// NamedID is the id of a target that has a name and no id of its own: the
// first 16 bytes of SHA-256 over kind and name, as a version 8 UUID, so
// the audit log finds every action on one provider or model by target_id.
func NamedID(kind TargetKind, name string) uuid.UUID {
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + name))
	var id uuid.UUID
	copy(id[:], sum[:16])
	id[6] = id[6]&0x0f | 0x80
	id[8] = id[8]&0x3f | 0x80
	return id
}

// Entry is one row of console_actions. Details holds what the action kind
// does not say, such as a suspension's reason; never a hash or a secret.
type Entry struct {
	Actor   Actor
	Action  ActionKind
	Target  Target
	Details map[string]any
	At      time.Time
}

const insertActions = `
INSERT INTO console_actions (id, actor_kind, member_id, actor_name, session_id, ip, action_kind, target_kind, target_id, details, acted_at)
SELECT id, actor_kind, member_id, actor_name, session_id, ip, action_kind, target_kind, target_id, details, acted_at
FROM unnest(@ids::uuid[], @actor_kinds::text[], @member_ids::uuid[], @actor_names::text[], @session_ids::uuid[],
    @ips::inet[], @action_kinds::text[], @target_kinds::text[], @target_ids::uuid[], @details::jsonb[], @acted_at::timestamptz[])
    AS a(id, actor_kind, member_id, actor_name, session_id, ip, action_kind, target_kind, target_id, details, acted_at)`

// Queue adds to b one statement that writes entries, so they commit with
// the change they record. No entries queues nothing. It is UC-SHR-02
// step 2.
func Queue(b *pgx.Batch, entries ...Entry) {
	if len(entries) == 0 {
		return
	}
	n := len(entries)
	ids, memberIDs, sessionIDs, targetIDs := make([]uuid.UUID, n), make([]any, n), make([]any, n), make([]uuid.UUID, n)
	actorKinds, actorNames, actionKinds, targetKinds := make([]string, n), make([]any, n), make([]string, n), make([]string, n)
	ips, details, actedAt := make([]any, n), make([]map[string]any, n), make([]time.Time, n)
	for i := range entries {
		e := &entries[i]
		ids[i], actorKinds[i], memberIDs[i] = uuid.NewV7(), string(e.Actor.Kind), nullID(e.Actor.MemberID)
		actorNames[i], sessionIDs[i], ips[i] = nullText(e.Actor.Name), nullID(e.Actor.SessionID), nullIP(e.Actor.IP)
		actionKinds[i], targetKinds[i], targetIDs[i] = string(e.Action), string(e.Target.Kind), e.Target.ID
		details[i], actedAt[i] = e.Details, e.At
		if details[i] == nil {
			details[i] = map[string]any{}
		}
	}
	b.Queue(insertActions, pgx.StrictNamedArgs{
		"ids": ids, "actor_kinds": actorKinds, "member_ids": memberIDs, "actor_names": actorNames, "session_ids": sessionIDs,
		"ips": ips, "action_kinds": actionKinds, "target_kinds": targetKinds, "target_ids": targetIDs, "details": details,
		"acted_at": actedAt,
	}).Fn = postgres.ExecAs("insert console actions", nil)
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
