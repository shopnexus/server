// Package service holds module agent's use cases: a console member's
// conversations with the console agent, whose turns run in the agent
// runtime; the agent's calls of the console API with its turn's token and
// the approvals of the changes it asks for; the AI settings; and the record
// of the reads of personal data on the console's record pages.
package service

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/bifrost"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// Service is module agent's contract.
type Service interface {
	// Conversations lists the conversations caller may open that q picks:
	// the one of a record, those about records of a kind, or all the
	// caller's own.
	//
	// UC-MOD-08
	Conversations(ctx context.Context, caller authn.ConsoleMember, q ConversationQuery) ([]Conversation, error)
	// CreateConversation returns the conversation of in's subject, shared by
	// every member and made the first time, or starts the caller's own when
	// in has no subject.
	//
	// UC-MOD-08
	CreateConversation(ctx context.Context, caller authn.ConsoleMember, in NewConversation) (Conversation, error)
	// Conversation reads a conversation with its messages and turns.
	//
	// UC-MOD-08
	Conversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConversationDetail, error)
	// RenameConversation gives a conversation a title: caller's own, or any
	// for an admin; a shared one is an admin's (agent_forbidden).
	//
	// UC-MOD-08
	RenameConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, title string) (Conversation, error)
	// DeleteConversation deletes a conversation with what it holds, by the
	// same rule as RenameConversation; a turn running refuses it.
	//
	// UC-MOD-08
	DeleteConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error
	// Ask adds caller's message to a conversation and starts a turn of a
	// console agent on it, which runs on in the background.
	//
	// UC-MOD-08
	Ask(ctx context.Context, caller authn.ConsoleMember, conversation uuid.UUID, in NewTurn) (TurnStarted, error)
	// Resume runs the conversation's last turn again where it ended.
	//
	// UC-MOD-08
	Resume(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID) (TurnState, error)
	// Pause asks a running turn to stop before its next model call, once
	// its commands finished; Stop ends it at once.
	//
	// UC-MOD-08
	Pause(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID) (TurnState, error)
	Stop(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID) (TurnState, error)
	// Steer gives a running turn a message to read before its next model
	// call.
	//
	// UC-MOD-08
	Steer(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, text string) (TurnState, error)
	// Turn reads a turn.
	//
	// UC-MOD-08
	Turn(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Turn, error)
	// Events calls emit with a turn's events after lastEventID until the
	// stream ends; an error before the first emit refuses the stream.
	//
	// UC-MOD-08
	Events(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, lastEventID string, emit func(TurnEvent) error) error
	// FollowTurns hands this replica's event streams the rings of new
	// events until ctx is done.
	FollowTurns(ctx context.Context)
	// SweepTurns closes the turns running past their deadline, whose run
	// this replica or another lost.
	SweepTurns(ctx context.Context) error
	// StopRuns cancels the runs this replica follows and waits for them to
	// end, as the server stops.
	StopRuns()

	// TurnOfToken reads the running turn a turn token belongs to;
	// domain.ErrTokenInvalid when none does.
	TurnOfToken(ctx context.Context, token string) (authn.Turn, error)
	// Operation is the operation a route pattern serves, when an agent
	// may be given it.
	Operation(method, pattern string) (domain.Operation, bool)
	// Operations lists the operations an agent may be given, or those its
	// turn holds when turn is set.
	//
	// UC-MOD-08
	Operations(ctx context.Context, caller authn.ConsoleMember, turn *authn.Turn) ([]domain.Operation, error)
	// Hold stores a change a turn's agent asked for as a pending approval.
	//
	// UC-MOD-08
	Hold(ctx context.Context, turn authn.Turn, op domain.Operation, req Request) (Approval, error)
	// RecordCall records an API call a turn's token made.
	RecordCall(ctx context.Context, turn authn.Turn, op domain.Operation, method, path string, status int, took time.Duration)
	// Approvals lists the approvals of the conversations caller may open,
	// or of turn's conversation when turn is set.
	//
	// UC-MOD-08
	Approvals(ctx context.Context, caller authn.ConsoleMember, turn *authn.Turn, q ApprovalQuery) ([]Approval, error)
	// Approve makes an approval's request through replay, as caller, and
	// stores what it answered; Reject only stores the decision.
	//
	// UC-MOD-08
	Approve(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, replay Replay) (Approval, error)
	Reject(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Approval, error)

	// ConsoleAgents lists the console agents a member may pick.
	ConsoleAgents(ctx context.Context) ([]ConsoleAgent, error)

	// The AI settings, an admin's. Each change is a console action.

	// Providers lists the providers Bifrost runs, with their status.
	//
	// UC-MOD-12
	Providers(ctx context.Context) ([]Provider, error)
	// ProviderModels lists a provider's catalog in Bifrost.
	//
	// UC-MOD-12
	ProviderModels(ctx context.Context, name string) ([]CatalogModel, error)
	// Models lists the models agents use or that have a price or tags.
	//
	// UC-MOD-12
	Models(ctx context.Context) (ModelList, error)
	// SetModelTags replaces a model's tags.
	//
	// UC-MOD-12 · UC-SHR-02
	SetModelTags(ctx context.Context, caller authn.ConsoleMember, in ModelTags) (ModelTags, error)
	// SetModelPrice writes a model's price.
	//
	// UC-MOD-12 · UC-SHR-02
	SetModelPrice(ctx context.Context, caller authn.ConsoleMember, in ModelPrice) (ModelPrice, error)
	// Agents lists the app and console agents.
	//
	// UC-MOD-12
	Agents(ctx context.Context) ([]AgentView, error)
	// CreateAgent makes a console agent.
	//
	// UC-MOD-12 A1 · UC-SHR-02
	CreateAgent(ctx context.Context, caller authn.ConsoleMember, in NewAgent) (AgentView, error)
	// UpdateAgent changes an agent.
	//
	// UC-MOD-12 · UC-SHR-02 · NFR-CONC-01
	UpdateAgent(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, in AgentPatch) (AgentView, error)
	// AgentVersions lists an agent's instructions' versions.
	//
	// UC-MOD-12 A3
	AgentVersions(ctx context.Context, id uuid.UUID) ([]AgentVersion, error)
	// Reconcile writes the agents, their callers and the prices into
	// Bifrost and hands the key ring the keys.
	//
	// UC-MOD-12
	Reconcile(ctx context.Context) error
	// Spend reads the daily spend from one date to another, in all and by
	// agent.
	//
	// UC-MOD-12 A4
	Spend(ctx context.Context, caller authn.ConsoleMember, from, to string) (Spend, error)
	// AgentUsage counts each agent's requests over a range.
	//
	// UC-MOD-12 A4
	AgentUsage(ctx context.Context, caller authn.ConsoleMember, rng string) ([]AgentUsage, error)
	// AgentActivity counts the console agents' calls, turns, approvals
	// and rate limit refusals over a range.
	//
	// UC-MOD-12 A4
	AgentActivity(ctx context.Context, caller authn.ConsoleMember, rng string) (Activity, error)
	// Order reads an order for the console's order page, recording the
	// read of its tickets for caller.
	Order(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConsoleOrder, error)
	// Listing reads a listing for the console's listing page, recording
	// the read of its reports for caller.
	Listing(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConsoleListing, error)
	// SweepReads deletes the reads and the agent's API calls older than
	// ReadsKeptFor, blanks the command outputs as old and deletes the
	// conversations idle for ConversationsKeptFor.
	//
	// NFR-PRIV-02
	SweepReads(ctx context.Context) error
	// AccountDeleted blanks the command outputs of the conversation about
	// a deleted account; the worker calls it on account.deleted, and a
	// second call finds nothing.
	AccountDeleted(ctx context.Context, account uuid.UUID) error
}

// ReadsKeptFor is how long a read of personal data stays recorded, and a
// command's output kept.
const ReadsKeptFor = 90 * 24 * time.Hour

// ConversationsKeptFor is how long a conversation stays after its last
// activity; then it goes with what it holds.
const ConversationsKeptFor = 180 * 24 * time.Hour

// Deps is what New builds Agent from.
type Deps struct {
	Reads         repository.Reads
	Settings      repository.Settings
	Conversations repository.Conversations
	// Ops is the console operations an agent may be given.
	Ops domain.Catalog
	// Gateway is Bifrost's admin API, and Ring the server's virtual keys,
	// which a turn's key comes from.
	Gateway bifrost.Admin
	Ring    *bifrost.Ring
	// Console names the console's members, and keeps the audit log.
	Console consolesvc.Service
	// The services the record pages read.
	Accounts accountsvc.Service
	Catalog  catalogsvc.Service
	Deals    dealsvc.Service
	Shipping shippingsvc.Service
	Support  supportsvc.Service
	Trust    trustsvc.Service
	// TextMin and PhotoMin are the similarity floors of the listing page's
	// price guide.
	TextMin, PhotoMin float64
	// RuntimeURL is the agent runtime's, and RuntimeKey the bearer key it
	// takes (AGENT_URL and AGENT_KEY).
	RuntimeURL string
	RuntimeKey string
	// HTTP calls the runtime; it has no timeout, as a turn's stream lasts
	// the turn.
	HTTP *http.Client
	Now  func() time.Time
	Log  *slog.Logger
}

// Agent implements Service in this process.
type Agent struct {
	d       Deps
	limiter *limiter
	hub     *hub
	// runs are the runs this replica follows, which StopRuns cancels.
	runs     sync.WaitGroup
	runsCtx  context.Context //nolint:containedctx // the runs outlive the requests that start them, until StopRuns.
	stopRuns context.CancelFunc
}

var _ Service = (*Agent)(nil)

func New(d Deps) *Agent {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Agent{d: d, limiter: newLimiter(), hub: newHub(), runsCtx: ctx, stopRuns: cancel}
}
