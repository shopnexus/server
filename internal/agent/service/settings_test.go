package service_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/agent/service"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/bifrost"
	"github.com/shopnexus/server/internal/provider/bifrost/bifrosttest"
)

var (
	settingsNow = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	scamCheck   = uuid.MustParse("0193e000-0000-7000-8000-00000000a102")
	search      = uuid.MustParse("0193e000-0000-7000-8000-00000000a104")
	embedText   = uuid.MustParse("0193e000-0000-7000-8000-00000000a105")
	haiku       = domain.ModelRef{Provider: "upstream", Model: "claude-haiku-5-5"}
	sonnet      = domain.ModelRef{Provider: "upstream", Model: "claude-sonnet-5-5"}
	gemini      = domain.ModelRef{Provider: "openrouter", Model: "google/gemini-2.5-flash"}
	qwen        = domain.ModelRef{Provider: "openrouter", Model: "qwen/qwen3-embedding-8b"}
)

// newBifrost is a fake Bifrost that runs the providers of config.json,
// upstream and openrouter, with a catalog.
func newBifrost(t *testing.T) *bifrosttest.Fake {
	t.Helper()
	f := bifrosttest.New()
	t.Cleanup(f.Close)
	f.SetProviders(
		bifrost.Provider{Name: "openrouter", BaseURL: "https://openrouter.ai/api", Status: "active"},
		bifrost.Provider{Name: "upstream", BaseURL: "http://10.0.0.9:8317/v1", Status: "active", Custom: "openai"},
	)
	f.SetCatalog("openrouter",
		bifrost.Model{Name: "google/gemini-2.5-flash", Provider: "openrouter", Input: new(3e-7), Output: new(2.5e-6), MaxInput: new(1048576)},
		bifrost.Model{Name: "baai/bge-m3", Provider: "openrouter", Input: new(1e-8), Output: new(0.0)},
		bifrost.Model{Name: "qwen/qwen3-embedding-8b", Provider: "openrouter", Input: new(1e-8), Output: new(0.0)},
	)
	f.SetCatalog("upstream", bifrost.Model{Name: "claude-haiku-5-5", Provider: "upstream"}, bifrost.Model{Name: "claude-sonnet-5-5", Provider: "upstream"})
	return f
}

// settingsOn builds the service on a migrated database and a fake
// Bifrost, with an admin who acts.
func settingsOn(t *testing.T) (*service.Agent, *bifrosttest.Fake, *pgxpool.Pool, authn.ConsoleMember) {
	t.Helper()
	db := pgtest.New(t)
	admin := authn.ConsoleMember{MemberID: uuid.NewV7(), SessionID: uuid.Nil(), Role: "admin"}
	if _, err := db.Exec(context.Background(), `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, 'Minh', 'admin', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": admin.MemberID, "email": admin.MemberID.String() + "@console.test", "at": settingsNow}); err != nil {
		t.Fatal(err)
	}
	seedRuns(t, db, admin.MemberID)
	f := newBifrost(t)
	s := service.New(service.Deps{
		Settings: repository.NewSettings(db), Runs: repository.NewRuns(db, slog.New(slog.DiscardHandler)),
		Gateway: f.Admin(), Ring: bifrost.NewRing(f.Admin(), time.Now),
		Console: members{list: []consolesvc.MemberView{
			{ID: admin.MemberID, DisplayName: "Minh", Role: "admin"}, {ID: moderator, DisplayName: "Lan", Role: "moderator"},
		}},
		Now: func() time.Time { return settingsNow },
	})
	return s, f, db, admin
}

// moderator is the members fake's moderator.
var moderator = uuid.MustParse("0193e000-0000-7000-8000-0000000000aa")

// seedRuns puts the Console assistant's turns in db: three of the
// moderator's at settingsNow, the first with a tool call and two guardrail
// events, and one of the admin's 30 days earlier.
func seedRuns(t *testing.T, db *pgxpool.Pool, admin uuid.UUID) {
	t.Helper()
	conversation, first := uuid.NewV7(), uuid.NewV7()
	at, agent := settingsNow, domain.ConsoleAssistant.String()
	for _, q := range []struct {
		sql  string
		args pgx.StrictNamedArgs
	}{
		{
			`INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@mod, 'lan@console.test', 'Lan', 'moderator', 'active', 'x', false, 1, 0, @at, @at, @mod)`,
			pgx.StrictNamedArgs{"mod": moderator, "at": at},
		},
		{`INSERT INTO agent_conversations (id, member_id, created_at, updated_at, last_activity_at)
VALUES (@c, @mod, @at, @at, @at)`, pgx.StrictNamedArgs{"c": conversation, "mod": moderator, "at": at}},
		{`INSERT INTO agent_turns (id, conversation_id, member_id, ordinal, status, question, agent_id, started_at, run_started_at,
    deadline_ms)
SELECT CASE WHEN n = 0 THEN @first::uuid ELSE uuidv7() END, @c, CASE WHEN n < 3 THEN @mod::uuid ELSE @admin::uuid END, n, 'succeeded', '?',
    @agent, CASE WHEN n < 3 THEN @at::timestamptz ELSE @at::timestamptz - interval '30 days' END, @at, 60000
FROM generate_series(0, 3) AS n`, pgx.StrictNamedArgs{
			"first": first, "c": conversation, "mod": moderator, "admin": admin, "agent": agent, "at": at,
		}},
		{
			`INSERT INTO agent_tool_calls (id, turn_id, tool_use_id, tool_name, tool_label, tool_input, status, latency_ms, created_at)
VALUES (@call, @first, 'tu1', 'ticket_get', 'Ticket', '{}', 'success', 80, @at)`,
			pgx.StrictNamedArgs{"call": uuid.NewV7(), "first": first, "at": at},
		},
		{
			`INSERT INTO agent_turn_events (turn_id, seq, event_type, body, occurred_at)
SELECT @first, n, 'guardrail', '{"rule":"loop_stopped","detail":"same call 3 times"}', @at FROM generate_series(1, 2) AS n`,
			pgx.StrictNamedArgs{"first": first, "at": at},
		},
	} {
		if _, err := db.Exec(context.Background(), q.sql, q.args); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
}

// members is console's member list.
type members struct {
	consolesvc.Service

	list []consolesvc.MemberView
}

func (m members) ListMembers(context.Context, page.Request) (page.Page[consolesvc.MemberView], error) {
	return page.Page[consolesvc.MemberView]{Items: m.list}, nil
}

// actions lists the console actions on the AI settings, oldest first.
func actions(t *testing.T, db *pgxpool.Pool) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT action_kind FROM console_actions WHERE action_kind LIKE 'ai.%' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The reconciler writes the seeded agents into Bifrost: a team per agent
// with its budget, a key per caller with its model, fallbacks and rate
// limits, and an override per price. It deletes what the server wrote that
// nothing names, keeps what others wrote, and a second run writes nothing.
func TestReconcile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, db, _ := settingsOn(t)
	f.PutKey(bifrost.VirtualKey{Name: "gone", Value: "sk-bf-gone", Metadata: map[string]string{bifrost.MetaOwner: bifrost.OwnerServer}})
	f.PutKey(bifrost.VirtualKey{Name: "someone", Value: "sk-bf-someone", Metadata: map[string]string{}})
	if err := f.Admin().CreatePricingOverride(ctx, "server:upstream/gone", "upstream", "gone", bifrost.Price{Input: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	teams := f.Teams()
	for slug, budget := range map[string]float64{
		"console-agent": 100, "scam-check": 30, "listing-read": 30, "search": 30, "embed-text": 30,
	} {
		if tm, ok := teams[slug]; !ok || len(tm.Budgets) != 1 || tm.Budgets[0].MaxLimit != budget || !tm.CalendarAligned {
			t.Errorf("team %s = %+v", slug, tm)
		}
	}
	keys := f.Keys()
	if _, ok := keys["gone"]; ok {
		t.Error("a key nothing names stayed")
	}
	if _, ok := keys["someone"]; !ok {
		t.Error("a key of another writer went")
	}
	trust := keys["server-trust"]
	if trust.TeamID == nil || *trust.TeamID != teams["scam-check"].ID || trust.RateLimit == nil ||
		*trust.RateLimit.TokenMaxLimit != 150000 || *trust.RateLimit.TokenResetDuration != "1m" || trust.RateLimit.RequestMaxLimit != nil ||
		trust.Metadata[bifrost.MetaModel] != "upstream/claude-haiku-5-5" || trust.Metadata[bifrost.MetaAgent] != "scam-check" ||
		len(trust.ProviderConfigs) != 1 || !trust.ProviderConfigs[0].AllowAllKeys ||
		!slices.Equal(trust.ProviderConfigs[0].AllowedModels, []string{"claude-haiku-5-5"}) {
		t.Errorf("server-trust = %+v", trust)
	}
	index := keys["server-embed-index"]
	if *index.RateLimit.RequestMaxLimit != 600 || index.Metadata[bifrost.MetaModel] != "openrouter/baai/bge-m3" ||
		!strings.Contains(index.Metadata[bifrost.MetaExtraBody], "DeepInfra") || *index.TeamID != *keys["server-embed-search"].TeamID {
		t.Errorf("server-embed-index = %+v", index)
	}
	overrides := f.Overrides()
	if _, ok := overrides["server:upstream/gone"]; ok || len(overrides) != 4 {
		t.Errorf("overrides = %v", overrides)
	}
	if o := overrides["server:upstream/claude-haiku-5-5"]; o["provider_id"] != "upstream" || o["pattern"] != "claude-haiku-5-5" {
		t.Errorf("haiku's override = %v", o)
	}
	writes := f.Writes()
	if err := s.Reconcile(ctx); err != nil || f.Writes() != writes {
		t.Errorf("a second run wrote %d times: %v", f.Writes()-writes, err)
	}
	// A key Bifrost's UI changed is put back; an agent turned off turns
	// its keys off.
	if err := f.Admin().UpdateVirtualKey(ctx, trust.ID, bifrost.KeyInput{
		Name: "server-trust", TeamID: *trust.TeamID, Models: map[string][]string{"upstream": {"claude-sonnet-5-5"}}, Active: true,
		Metadata: trust.Metadata,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE ai_agents SET enabled = false WHERE slug = 'listing-read'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	keys = f.Keys()
	if got := keys["server-trust"]; got.RateLimit == nil || !slices.Equal(got.ProviderConfigs[0].AllowedModels, []string{"claude-haiku-5-5"}) {
		t.Errorf("server-trust after an edit in Bifrost = %+v", got)
	}
	if keys["server-assist"].Active() {
		t.Error("a turned off agent's key is on")
	}
}

func TestAgentRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, db, admin := settingsOn(t)
	for name, tc := range map[string]struct {
		in   service.NewAgent
		want error
	}{
		"lacks tags":   {service.NewAgent{Name: "Thinker", Slug: "thinker", RequiredTags: []string{"Reasoning"}, Model: haiku}, domain.ErrModelLacksTags},
		"unpriced":     {service.NewAgent{Name: "Qwen", Slug: "qwen", Model: qwen}, domain.ErrModelUnpriced},
		"name taken":   {service.NewAgent{Name: "Console assistant", Slug: "other", Model: haiku}, domain.ErrAgentTaken},
		"bad slug":     {service.NewAgent{Name: "Bad", Slug: "Bad Slug", Model: haiku}, domain.ErrAgentInvalid},
		"zero budget":  {service.NewAgent{Name: "Free", Slug: "free", Model: haiku, MonthlyBudgetUSD: new(0.0)}, domain.ErrAgentInvalid},
		"slow":         {service.NewAgent{Name: "Slow", Slug: "slow", Model: haiku, Limits: domain.Limits{DeadlineS: 301}}, domain.ErrAgentInvalid},
		"no provider":  {service.NewAgent{Name: "Lost", Slug: "lost", Model: domain.ModelRef{Provider: "gone", Model: "x"}}, domain.ErrModelUnpriced},
		"twice":        {service.NewAgent{Name: "Twice", Slug: "twice", Model: haiku, Fallbacks: []domain.ModelRef{haiku}}, domain.ErrAgentInvalid},
		"fallback tag": {service.NewAgent{Name: "Fb", Slug: "fb", RequiredTags: []string{"Chat"}, Model: sonnet, Fallbacks: []domain.ModelRef{{Provider: "openrouter", Model: "baai/bge-m3"}}}, domain.ErrModelLacksTags},
	} {
		if _, err := s.CreateAgent(ctx, admin, tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	if _, err := s.UpdateAgent(ctx, admin, scamCheck, service.AgentPatch{Name: new("Scams")}); !errors.Is(err, domain.ErrAppAgentFixed) {
		t.Errorf("an app agent's name: %v", err)
	}
	if _, err := s.UpdateAgent(ctx, admin, embedText, service.AgentPatch{
		Fallbacks: &[]domain.ModelRef{{Provider: "openrouter", Model: "baai/bge-m3"}},
	}); !errors.Is(err, domain.ErrAgentInvalid) {
		t.Errorf("an embedding agent's fallback: %v", err)
	}
	for name, callers := range map[string][]service.Caller{
		"unknown": {{Name: "server-trust"}, {Name: "other"}},
		"missing": {},
		"twice":   {{Name: "server-trust"}, {Name: "server-trust"}},
	} {
		if _, err := s.UpdateAgent(ctx, admin, scamCheck, service.AgentPatch{Callers: &callers}); !errors.Is(err, domain.ErrAgentInvalid) {
			t.Errorf("callers %s: %v", name, err)
		}
	}
	got, err := s.UpdateAgent(ctx, admin, scamCheck, service.AgentPatch{
		Model: &sonnet, Fallbacks: &[]domain.ModelRef{haiku}, MonthlyBudgetUSD: service.Budget{Set: true, Value: new(10.0)},
		Callers: &[]service.Caller{{Name: "server-trust", RequestsPerMin: new(60)}},
	})
	if err != nil || got.Model != sonnet || !slices.Equal(got.Fallbacks, []domain.ModelRef{haiku}) || *got.MonthlyBudgetUSD != 10 ||
		len(got.Callers) != 1 || *got.Callers[0].RequestsPerMin != 60 || got.Callers[0].TokensPerMin != nil {
		t.Fatalf("an app agent's model, fallbacks, budget and callers: %+v %v", got, err)
	}
	k := f.Keys()["server-trust"]
	if k.Metadata[bifrost.MetaModel] != "upstream/claude-sonnet-5-5" || k.Metadata[bifrost.MetaFallbacks] != "upstream/claude-haiku-5-5" ||
		*k.RateLimit.RequestMaxLimit != 60 || k.RateLimit.TokenMaxLimit != nil ||
		!sameModels(k.ProviderConfigs[0].AllowedModels, "claude-sonnet-5-5", "claude-haiku-5-5") {
		t.Errorf("server-trust after the change = %+v", k)
	}
	if tm := f.Teams()["scam-check"]; tm.Budgets[0].MaxLimit != 10 {
		t.Errorf("scam-check's team = %+v", tm)
	}
	f.SetSpent("scam-check", 2.5)
	views, err := s.Agents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(views, func(v service.AgentView) bool { return v.ID == scamCheck })
	if v := views[i]; v.BudgetSpentUSD == nil || *v.BudgetSpentUSD != 2.5 || !v.BudgetResetsAt.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("scam-check's spend %+v", v)
	}

	// A new model drops the old one's request fields.
	if _, err = s.SetModelTags(ctx, admin, service.ModelTags{Provider: qwen.Provider, Model: qwen.Model, Tags: []string{"Embedding", "Multilingual"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetModelPrice(ctx, admin, service.ModelPrice{Provider: qwen.Provider, Model: qwen.Model, Price: service.Price{Input: 1e-8}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateAgent(ctx, admin, embedText, service.AgentPatch{Model: &qwen}); err != nil {
		t.Fatal(err)
	}
	if meta := f.Keys()["server-embed-index"].Metadata; meta[bifrost.MetaModel] != qwen.String() || meta[bifrost.MetaExtraBody] != "" {
		t.Errorf("server-embed-index after a new model = %v", meta)
	}

	created, err := s.CreateAgent(ctx, admin, service.NewAgent{
		Name: "Writer", Slug: "writer", Model: haiku, Fallbacks: []domain.ModelRef{sonnet}, MonthlyBudgetUSD: new(4.0),
	})
	if err != nil || created.Kind != domain.AgentConsole || len(created.Callers) != 1 || created.Callers[0].Name != "writer" {
		t.Fatalf("a console agent %+v %v", created, err)
	}
	if k, ok := f.Keys()["writer"]; !ok || k.Metadata[bifrost.MetaAgent] != "writer" {
		t.Errorf("the writer's key %+v", k)
	}

	got, err = s.UpdateAgent(ctx, admin, domain.ConsoleAssistant, service.AgentPatch{Instructions: new("Be brief.")})
	if err != nil || got.InstructionsVersion != 2 {
		t.Fatalf("new instructions %+v %v", got, err)
	}
	versions, err := s.AgentVersions(ctx, domain.ConsoleAssistant)
	if err != nil || len(versions) != 2 || versions[0].Version != 2 || versions[0].Instructions != "Be brief." ||
		*versions[0].CreatedBy != admin.MemberID || versions[1].CreatedBy != nil {
		t.Fatalf("versions %+v %v", versions, err)
	}
	// A version that exists with other text refuses the save.
	if _, err = db.Exec(ctx, `INSERT INTO ai_agent_versions (agent_id, version_no, instructions, created_at)
VALUES (@id, 3, 'Stray.', now())`, pgx.StrictNamedArgs{"id": domain.ConsoleAssistant}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateAgent(ctx, admin, domain.ConsoleAssistant, service.AgentPatch{Instructions: new("Be kind.")}); err == nil {
		t.Error("a clashing version was saved")
	}
	if _, err = s.UpdateAgent(ctx, admin, uuid.NewV7(), service.AgentPatch{}); !errors.Is(err, domain.ErrAgentNotFound) {
		t.Errorf("an unknown agent: %v", err)
	}

	picks, err := s.ConsoleAgents(ctx)
	if err != nil || len(picks) != 2 || !slices.ContainsFunc(picks, func(p service.ConsoleAgent) bool { return p.Default }) {
		t.Fatalf("the agents a member picks %+v %v", picks, err)
	}
	if _, err = s.UpdateAgent(ctx, admin, domain.ConsoleAssistant, service.AgentPatch{Enabled: new(false)}); err != nil {
		t.Fatal(err)
	}
	if f.Keys()["console-agent"].Active() {
		t.Error("a turned off agent's key is on")
	}
	if _, err = s.Ask(ctx, admin, uuid.NewV7(), service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrAgentDisabled) {
		t.Errorf("a question to a turned off agent: %v", err)
	}
	if got := actions(t, db); !slices.Contains(got, "ai.model_tags") || !slices.Contains(got, "ai.model_price") ||
		!slices.Contains(got, "ai.agent_update") || !slices.Contains(got, "ai.agent_create") {
		t.Errorf("console actions %v", got)
	}
}

func sameModels(got []string, want ...string) bool {
	return len(got) == len(want) && !slices.ContainsFunc(want, func(m string) bool { return !slices.Contains(got, m) })
}

// The models are those agents use, priced or tagged, each with its catalog
// price; a price is saved, then written into Bifrost.
func TestModelsAndPrices(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, _, admin := settingsOn(t)
	if _, err := s.SetModelTags(ctx, admin, service.ModelTags{Provider: qwen.Provider, Model: qwen.Model, Tags: []string{"Embedding", "Embedding"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetModelTags(ctx, admin, service.ModelTags{Provider: qwen.Provider, Model: qwen.Model, Tags: []string{"Fast"}}); !errors.Is(err, domain.ErrTagUnknown) {
		t.Errorf("an unknown tag: %v", err)
	}
	list, err := s.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	find := func(m domain.ModelRef) service.Model {
		i := slices.IndexFunc(list.Items, func(x service.Model) bool { return x.Provider == m.Provider && x.Model == m.Model })
		if i < 0 {
			t.Fatalf("%s is not listed: %+v", m, list.Items)
		}
		return list.Items[i]
	}
	if q := find(qwen); q.Price != nil || q.CatalogPrice == nil || q.CatalogPrice.Input != 1e-8 || !slices.Equal(q.Tags, []string{"Embedding"}) {
		t.Errorf("a tagged model %+v", q)
	}
	if g := find(gemini); g.Price == nil || g.Price.Input != 3e-7 || g.CatalogPrice == nil || len(g.Agents) != 1 || g.Agents[0].ID != search {
		t.Errorf("search's model %+v", g)
	}
	if h := find(haiku); h.CatalogPrice != nil || len(h.Agents) != 2 {
		t.Errorf("haiku %+v", h)
	}
	if len(list.Tags) == 0 {
		t.Error("no tags")
	}
	if _, err = s.SetModelPrice(ctx, admin, service.ModelPrice{Provider: "gone", Model: "x", Price: service.Price{Input: 1}}); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("an unknown provider: %v", err)
	}
	if _, err = s.SetModelPrice(ctx, admin, service.ModelPrice{Provider: qwen.Provider, Model: qwen.Model, Price: service.Price{Input: -1}}); !errors.Is(err, domain.ErrPriceInvalid) {
		t.Errorf("a price under 0: %v", err)
	}
	in := service.ModelPrice{Provider: qwen.Provider, Model: qwen.Model, Price: service.Price{Input: 2e-8, Output: 0, CacheRead: new(1e-9)}}
	got, err := s.SetModelPrice(ctx, admin, in)
	if err != nil || got.Input != 2e-8 {
		t.Fatalf("a price %+v %v", got, err)
	}
	patch, _ := f.Overrides()["server:openrouter/qwen/qwen3-embedding-8b"]["patch"].(map[string]any)
	if patch["input_cost_per_token"] != 2e-8 || patch["cache_read_input_token_cost"] != 1e-9 || patch["cache_creation_input_token_cost"] != 2e-8 {
		t.Errorf("qwen's override %v", patch)
	}
	// Bifrost down: the price is saved and the next reconcile writes it.
	f.Fail("PUT /api/governance/pricing-overrides/" + overrideID(t, f, "server:openrouter/qwen/qwen3-embedding-8b"))
	in.Input = 3e-8
	if _, err = s.SetModelPrice(ctx, admin, in); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Fatalf("a price Bifrost fails: %v", err)
	}
	f.Fail()
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	patch, _ = f.Overrides()["server:openrouter/qwen/qwen3-embedding-8b"]["patch"].(map[string]any)
	if patch["input_cost_per_token"] != 3e-8 {
		t.Errorf("qwen's override after the reconcile %v", patch)
	}
}

func overrideID(t *testing.T, f *bifrosttest.Fake, name string) string {
	t.Helper()
	id, _ := f.Overrides()[name]["id"].(string)
	if id == "" {
		t.Fatalf("no override %s", name)
	}
	return id
}

// A provider's status follows its last hour: unknown without requests,
// down from half failed, degraded from 5% with enough requests, and down
// when Bifrost could not start it.
func TestProviders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, _, _ := settingsOn(t)
	f.SetProviders(
		bifrost.Provider{Name: "openrouter", BaseURL: "https://openrouter.ai/api", Status: "active"},
		bifrost.Provider{Name: "upstream", BaseURL: "http://10.0.0.9:8317/v1", Status: "active", Custom: "openai"},
		bifrost.Provider{Name: "anthropic", Status: "active"},
		bifrost.Provider{Name: "broken", Status: "error"},
		bifrost.Provider{Name: "slow", Status: "active"},
	)
	f.SetStats("openrouter", 100, 99.5)
	f.SetStats("upstream", 40, 90)
	f.SetStats("slow", 6, 40)
	list, err := s.Providers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"anthropic": "unknown", "broken": "down", "openrouter": "ok", "slow": "down", "upstream": "degraded"}
	for _, p := range list {
		if p.Status != want[p.Name] {
			t.Errorf("%s is %s, want %s", p.Name, p.Status, want[p.Name])
		}
	}
	if list[3].Name != "slow" || list[4].Name != "upstream" || list[4].Kind != "custom" || list[4].BaseHost != "10.0.0.9:8317" ||
		list[4].ModelCount != 2 || list[4].Requests1h != 40 || list[4].ErrorShare1h != 0.1 || list[2].Kind != "openrouter" {
		t.Errorf("providers %+v", list)
	}
	models, err := s.ProviderModels(ctx, "openrouter")
	if err != nil || len(models) != 3 || models[1].Model != "google/gemini-2.5-flash" || models[1].CatalogPrice.Output != 2.5e-6 ||
		*models[1].MaxInputTokens != 1048576 {
		t.Fatalf("openrouter's models %+v %v", models, err)
	}
	if _, err = s.ProviderModels(ctx, "gone"); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("an unknown provider: %v", err)
	}
	f.SetDown(true)
	if _, err = s.Providers(ctx); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Errorf("Bifrost down: %v", err)
	}
}

// Spend sums Bifrost's cost by UTC day and agent over windows Bifrost
// answers by the day; usage counts an app agent's requests by its team and
// a console agent's turns.
func TestSpendUsageToolsAndGuardrails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, _, admin := settingsOn(t)
	if err := s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Admin().CreateTeam(ctx, "someone", nil); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := range 60 {
		f.Log(day.AddDate(0, 0, i).Add(13*time.Hour), "scam-check", 0.5, 1)
	}
	f.Log(day.Add(23*time.Hour+59*time.Minute), "search", 1, 2)
	f.Log(day.Add(time.Hour), "someone", 2, 1)
	f.Log(settingsNow.Add(-time.Hour), "scam-check", 0.25, 7)
	spend, err := s.Spend(ctx, admin, "2026-09-01", "2026-10-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(spend.Days) != 60 || spend.LagS != 60 || spend.Days[0].Date != "2026-09-01" || spend.Days[0].TotalUSD != 3.5 ||
		spend.Days[0].ByAgent["scam-check"] != 0.5 || spend.Days[0].ByAgent["search"] != 1 || len(spend.Days[0].ByAgent) != 2 ||
		spend.Days[59].ByAgent["scam-check"] != 0.5 || spend.Days[40].TotalUSD != 0.5 {
		t.Errorf("spend %+v", spend.Days[:2])
	}
	for _, rng := range [][2]string{{"2026-10-02", "2026-10-01"}, {"2025-01-01", "2026-10-01"}, {"soon", "2026-10-01"}} {
		if _, err = s.Spend(ctx, admin, rng[0], rng[1]); !errors.Is(err, domain.ErrSpendRange) {
			t.Errorf("spend %v: %v", rng, err)
		}
	}
	usage, err := s.AgentUsage(ctx, admin, "")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[uuid.UUID]int{domain.ConsoleAssistant: 3, scamCheck: 7} {
		i := slices.IndexFunc(usage, func(u service.AgentUsage) bool { return u.AgentID == id })
		if i < 0 || usage[i].Requests != want || usage[i].Days[len(usage[i].Days)-1].Requests != want {
			t.Errorf("usage of %s: %+v", id, usage)
		}
	}
	tools, err := s.Tools(ctx, admin, "7d")
	if err != nil || len(tools.Items) != 1 || tools.Items[0].DisplayName != "Ticket" || tools.Items[0].Calls != 1 ||
		*tools.Items[0].P50MS != 80 || len(tools.Items[0].Days) != 7 || len(tools.Recent) != 1 || tools.Recent[0].MemberID != moderator {
		t.Fatalf("tools %+v %v", tools, err)
	}
	guards, err := s.Guardrails(ctx, admin, "bogus")
	if err != nil || len(guards.Items) != 1 || guards.Items[0].Count != 2 || len(guards.Recent) != 2 {
		t.Fatalf("guardrails %+v %v", guards, err)
	}
}
