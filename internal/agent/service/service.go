// Package service holds module agent's use cases: a console member's
// conversations with the console agent, whose runs the agent service
// streams back to the server, the AI settings, and the record of what the
// agent read of people through the MCP server.
package service

import (
	"context"
	"log/slog"
	"net/http"
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
	// the one of a record, those about records of a kind, the caller's own
	// that mention a record, or all the caller's own.
	//
	// UC-MOD-08
	Conversations(ctx context.Context, caller authn.ConsoleMember, q ConversationQuery) ([]Conversation, error)
	// CreateConversation returns the conversation of in's subject, shared by
	// every member and made the first time, or starts the caller's own when
	// in has no subject.
	//
	// UC-MOD-08
	CreateConversation(ctx context.Context, caller authn.ConsoleMember, in NewConversation) (Conversation, error)
	// Conversation reads a conversation with its turns.
	//
	// UC-MOD-08
	Conversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConversationDetail, error)
	// RenameConversation gives a conversation a title: caller's own, or any
	// for an admin; a shared one is an admin's (agent_forbidden).
	//
	// UC-MOD-08
	RenameConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, title string) (Conversation, error)
	// DeleteConversation deletes a conversation with its turns, events and
	// documents, by the same rule as RenameConversation; a turn still open
	// refuses it (agent_turn_in_progress).
	//
	// UC-MOD-08
	DeleteConversation(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error
	// Ask puts caller's question to a console agent in a conversation and
	// starts its run, which goes on in the background.
	//
	// UC-MOD-08 · BR-MOD-03 · ADR-0003
	Ask(ctx context.Context, caller authn.ConsoleMember, conversation uuid.UUID, in NewTurn) (TurnStarted, error)
	// Events calls emit with a turn's events after lastEventID until the
	// stream ends; an error before the first emit refuses the stream.
	//
	// UC-MOD-08
	Events(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, lastEventID string, emit func(TurnEvent) error) error
	// Control pauses, stops, continues or ends a turn's run.
	//
	// UC-MOD-08 A1, A2
	Control(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, c Control) (TurnState, error)
	// Steer gives a run text to read before its next model call; a key
	// caller used answers its steer.
	//
	// UC-MOD-08 A3
	Steer(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, text string, key *uuid.UUID) (Steering, error)
	// Turn reads a turn.
	//
	// UC-MOD-08
	Turn(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Turn, error)
	// Answer answers a turn's clarifying questions.
	//
	// UC-MOD-08 A4
	Answer(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, answers Answers) (TurnState, error)
	// TurnControl is what a running segment polls through the MCP
	// listener: the turn's pauses, stops and steers after a seq.
	TurnControl(ctx context.Context, turn uuid.UUID, after int64) (TurnControl, error)
	// FollowTurns hands this replica's event streams the rings of new
	// events until ctx is done.
	FollowTurns(ctx context.Context)
	// SweepTurns closes the turns running past their deadline as
	// agent_stuck.
	//
	// UC-MOD-08
	SweepTurns(ctx context.Context) error
	// ToolCall reads a tool call in full.
	ToolCall(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ToolCall, error)
	// Document reads a document a turn wrote, with its versions.
	Document(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Document, error)
	// Skill reads a skill.
	Skill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (Skill, error)
	// Skills lists the skills every member shares.
	Skills(ctx context.Context, caller authn.ConsoleMember) ([]Skill, error)
	// CreateSkill saves a skill.
	CreateSkill(ctx context.Context, caller authn.ConsoleMember, in SkillInput) (Skill, error)
	// UpdateSkill changes a skill.
	UpdateSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, in SkillPatch) (Skill, error)
	// DeleteSkill deletes a skill.
	DeleteSkill(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) error
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
	// Tools reads the agent's tool calls over a range.
	//
	// UC-MOD-12 A4
	Tools(ctx context.Context, caller authn.ConsoleMember, rng string) (ToolStats, error)
	// Order reads an order for the console's order page, recording the
	// read of its tickets for caller.
	Order(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConsoleOrder, error)
	// Listing reads a listing for the console's listing page, recording
	// the read of its reports for caller.
	Listing(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID) (ConsoleListing, error)
	// Guardrails reads the agent's guardrail notices over a range.
	//
	// UC-MOD-12 A4
	Guardrails(ctx context.Context, caller authn.ConsoleMember, rng string) (GuardrailStats, error)
	// RecordRead records that an MCP tool read personal data of subject
	// for member.
	//
	// UC-MOD-08 · NFR-PRIV-02
	RecordRead(ctx context.Context, member uuid.UUID, tool string, kind domain.SubjectKind, ids []uuid.UUID) error
	// SweepReads deletes the reads older than ReadsKeptFor, blanks the
	// tool outputs as old and deletes the conversations idle for
	// ConversationsKeptFor.
	//
	// NFR-PRIV-02
	SweepReads(ctx context.Context) error
	// AccountDeleted blanks the tool outputs of the conversation about a
	// deleted account; the worker calls it on account.deleted, and a
	// second call finds nothing.
	AccountDeleted(ctx context.Context, account uuid.UUID) error
}

// ReadsKeptFor is how long a read of personal data stays recorded, and a
// tool call's output kept.
const ReadsKeptFor = 90 * 24 * time.Hour

// ConversationsKeptFor is how long a conversation stays after its last
// activity; then it goes with its turns and documents.
const ConversationsKeptFor = 180 * 24 * time.Hour

// Deps is what New builds Agent from.
type Deps struct {
	Reads    repository.Reads
	Settings repository.Settings
	Runs     repository.Runs
	// Gateway is Bifrost's admin API, and Ring the server's virtual keys,
	// which a console run's key comes from.
	Gateway bifrost.Admin
	Ring    *bifrost.Ring
	// Console lists the console's members, for their names, and the audit
	// log.
	Console consolesvc.Service
	// The services the record pages read, as the MCP tools do, and the
	// similarity floors of the price guide.
	Accounts          accountsvc.Service
	Catalog           catalogsvc.Service
	Deals             dealsvc.Service
	Shipping          shippingsvc.Service
	Support           supportsvc.Service
	Trust             trustsvc.Service
	TextMin, PhotoMin float64
	// BaseURL is the agent service's, and Token the bearer token it takes
	// (AGENT_BASE_URL and AGENT_SERVICE_TOKEN).
	BaseURL string
	Token   string
	HTTP    *http.Client
	Now     func() time.Time
	Log     *slog.Logger
}

// Agent implements Service in this process.
type Agent struct {
	d       Deps
	limiter *limiter
	hub     *hub
}

var _ Service = (*Agent)(nil)

func New(d Deps) *Agent {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Agent{d: d, limiter: newLimiter(), hub: newHub()}
}
