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
)

var (
	settingsNow = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	scamCheck   = uuid.MustParse("0193e000-0000-7000-8000-00000000a102")
)

// settingsOn builds the service on a migrated database and a fake
// LiteLLM, with an admin who acts.
func settingsOn(t *testing.T) (*service.Agent, *fakeLiteLLM, *pgxpool.Pool, authn.ConsoleMember) {
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
	f := newFakeLiteLLM(t)
	s := service.New(service.Deps{
		Settings: repository.NewSettings(db), Runs: repository.NewRuns(db, slog.New(slog.DiscardHandler)), Gateway: f.admin(),
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

// Each provider that serves a model is one deployment of an agent's alias,
// with the provider's weight and order; a new weight is written onto the
// alias, and the last provider of an agent's model cannot go.
func TestProvidersBecomeTheAliasDeployments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, db, admin := settingsOn(t)
	for _, p := range []service.NewProvider{
		{ID: "relay", Name: "Relay", Kind: "openai", BaseURL: "https://relay.example/v1", APIKey: "sk-relay", Weight: new(2.0), Order: new(1)},
		{ID: "backup", Kind: "openrouter", APIKey: "sk-backup", Order: new(2)},
	} {
		if _, err := s.CreateProvider(ctx, admin, p); err != nil {
			t.Fatalf("create %s: %v", p.ID, err)
		}
	}
	if _, err := s.CreateProvider(ctx, admin, service.NewProvider{ID: "relay", Kind: "openai", APIKey: "x"}); !errors.Is(err, domain.ErrProviderExists) {
		t.Errorf("the same id again: %v", err)
	}
	if _, err := s.CreateProvider(ctx, admin, service.NewProvider{ID: "Bad Id", Kind: "openai", APIKey: "x"}); !errors.Is(err, domain.ErrProviderInvalid) {
		t.Errorf("a bad id: %v", err)
	}
	for _, id := range []string{"relay", "backup"} {
		if _, err := s.AddProviderModel(ctx, admin, id, "claude-haiku-5-5", new(1.0), new(5.0)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.Providers(ctx)
	if err != nil || len(list) != 2 || list[1].ID != "relay" || list[1].Name != "Relay" || list[1].Weight != 2 ||
		!slices.Equal(list[1].Models, []string{"claude-haiku-5-5"}) || list[1].KeyHint != "sk-****" || list[0].Name != "backup" {
		t.Fatalf("providers %+v %v", list, err)
	}

	created, err := s.CreateAgent(ctx, admin, service.NewAgent{
		Name: "Report writer", Alias: "report-writer", Icon: "doc", Purpose: "Writes reports.", RequiredTags: []string{"Chat"},
		Model: "claude-haiku-5-5", Instructions: "Write a weekly report.", Tools: []string{"count"},
		Limits: domain.Limits{DeadlineS: 240}, MonthlyBudgetUSD: new(5.0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.aliases("report-writer"); !slices.Equal(got, []string{
		"backup:openrouter/claude-haiku-5-5:1:2", "relay:openai/claude-haiku-5-5:2:1",
	}) {
		t.Fatalf("alias deployments %v", got)
	}
	key := f.key("agent")
	if models, ok := key["models"].([]any); !ok || !slices.Contains(models, any("report-writer")) {
		t.Errorf("the agent key's models %v", key["models"])
	}
	if per, ok := key["model_max_budget"].(map[string]any); !ok || per["report-writer"] == nil {
		t.Errorf("the alias's budget %v", key["model_max_budget"])
	}
	if created.Kind != domain.AgentConsole || created.Limits.DeadlineS != 240 || created.InstructionsVersion != 1 {
		t.Errorf("created %+v", created)
	}

	if _, err = s.UpdateProvider(ctx, admin, "backup", service.ProviderPatch{Weight: new(3.0)}); err != nil {
		t.Fatal(err)
	}
	if got := f.aliases("report-writer"); !slices.Equal(got, []string{
		"backup:openrouter/claude-haiku-5-5:3:2", "relay:openai/claude-haiku-5-5:2:1",
	}) {
		t.Fatalf("after a new weight %v", got)
	}
	if _, err = s.RemoveProviderModel(ctx, admin, "backup", "claude-haiku-5-5"); err != nil {
		t.Fatal(err)
	}
	if got := f.aliases("report-writer"); len(got) != 1 {
		t.Fatalf("after the model left backup %v", got)
	}
	if _, err = s.RemoveProviderModel(ctx, admin, "relay", "claude-haiku-5-5"); !errors.Is(err, domain.ErrProviderInUse) {
		t.Errorf("the last provider's model: %v", err)
	}
	if err = s.DeleteProvider(ctx, admin, "relay"); !errors.Is(err, domain.ErrProviderInUse) {
		t.Errorf("the last provider: %v", err)
	}
	if err = s.DeleteProvider(ctx, admin, "backup"); err != nil {
		t.Fatal(err)
	}
	test, err := s.TestProvider(ctx, "relay", "")
	if err != nil || !test.OK || test.Model != "claude-haiku-5-5" {
		t.Errorf("test %+v %v", test, err)
	}

	models, err := s.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(models.Items, func(m service.Model) bool { return m.Model == "claude-haiku-5-5" })
	if i < 0 || len(models.Tags) != 8 || !slices.Contains(models.Items[i].Tags, "Vision") ||
		len(models.Items[i].Providers) != 1 || *models.Items[i].InputCostPerMTok != 1 ||
		!slices.ContainsFunc(models.Items[i].Agents, func(a service.AgentRef) bool { return a.Name == "Report writer" }) {
		t.Fatalf("models %+v", models)
	}
	want := []string{
		"ai.provider_create", "ai.provider_create", "ai.provider_models", "ai.provider_models", "ai.agent_create",
		"ai.provider_update", "ai.provider_models", "ai.provider_delete",
	}
	if got := actions(t, db); !slices.Equal(got, want) {
		t.Errorf("console actions %v, want %v", got, want)
	}
}

// An agent's model carries its required tags; an app agent changes only
// its model, limits and budget; new instructions are a new version; a
// member picks among the console agents turned on.
func TestAgentRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, db, admin := settingsOn(t)
	if _, err := s.CreateProvider(ctx, admin, service.NewProvider{ID: "relay", Kind: "openai", APIKey: "sk-relay"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"claude-haiku-5-5", "baai/bge-m3"} {
		if _, err := s.AddProviderModel(ctx, admin, "relay", m, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateAgent(ctx, admin, service.NewAgent{
		Name: "Photo agent", Alias: "photo-agent", RequiredTags: []string{"Chat", "Vision"}, Model: "baai/bge-m3",
	}); !errors.Is(err, domain.ErrModelLacksTags) {
		t.Errorf("a model without the tags: %v", err)
	}
	if _, err := s.CreateAgent(ctx, admin, service.NewAgent{
		Name: "Console assistant", Alias: "other-alias", Model: "claude-haiku-5-5",
	}); !errors.Is(err, domain.ErrAgentTaken) {
		t.Errorf("a name taken: %v", err)
	}
	if _, err := s.CreateAgent(ctx, admin, service.NewAgent{
		Name: "Slow", Alias: "slow-agent", Model: "claude-haiku-5-5", Limits: domain.Limits{DeadlineS: 301},
	}); !errors.Is(err, domain.ErrAgentInvalid) {
		t.Errorf("a deadline past 300 s: %v", err)
	}
	if _, err := s.UpdateAgent(ctx, admin, scamCheck, service.AgentPatch{Name: new("Scams")}); !errors.Is(err, domain.ErrAppAgentFixed) {
		t.Errorf("an app agent's name: %v", err)
	}
	if _, err := s.UpdateAgent(ctx, admin, scamCheck, service.AgentPatch{Model: new("baai/bge-m3")}); !errors.Is(err, domain.ErrModelLacksTags) {
		t.Errorf("an app agent on an embedding model: %v", err)
	}
	got, err := s.UpdateAgent(ctx, admin, scamCheck, service.AgentPatch{
		Model: new("claude-haiku-5-5"), MonthlyBudgetUSD: service.Budget{Set: true, Value: new(10.0)},
	})
	if err != nil || got.Model != "claude-haiku-5-5" || *got.MonthlyBudgetUSD != 10 {
		t.Fatalf("an app agent's model and budget: %+v %v", got, err)
	}
	if deps := f.aliases("scam-check"); len(deps) != 1 {
		t.Errorf("scam-check's deployments %v", deps)
	}
	if k := f.key("server-trust"); k["max_budget"] != 10.0 || k["budget_duration"] != "30d" {
		t.Errorf("server-trust's budget %v", k)
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
	if a, readErr := s.Agents(ctx); readErr != nil || !slices.ContainsFunc(a, func(v service.AgentView) bool {
		return v.ID == domain.ConsoleAssistant && v.InstructionsVersion == 2 && v.Instructions == "Be brief."
	}) {
		t.Errorf("the agent after a clash %+v %v", a, readErr)
	}
	if _, err = s.UpdateAgent(ctx, admin, uuid.NewV7(), service.AgentPatch{}); !errors.Is(err, domain.ErrAgentNotFound) {
		t.Errorf("an unknown agent: %v", err)
	}

	picks, err := s.ConsoleAgents(ctx)
	if err != nil || len(picks) != 1 || !picks[0].Default || picks[0].Name != "Console assistant" {
		t.Fatalf("the agents a member picks %+v %v", picks, err)
	}
	if _, err = s.UpdateAgent(ctx, admin, domain.ConsoleAssistant, service.AgentPatch{Enabled: new(false)}); err != nil {
		t.Fatal(err)
	}
	if picks, _ = s.ConsoleAgents(ctx); len(picks) != 0 {
		t.Errorf("a turned off agent is picked: %+v", picks)
	}
	if _, err = s.Ask(ctx, admin, uuid.NewV7(), service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrAgentDisabled) {
		t.Errorf("a question to a turned off agent: %v", err)
	}

	tags, err := s.SetModelTags(ctx, admin, "baai/bge-m3", []string{"Embedding", "Long context"})
	if err != nil || !slices.Equal(tags, []string{"Embedding", "Long context"}) {
		t.Errorf("tags %v %v", tags, err)
	}
	if _, err = s.SetModelTags(ctx, admin, "baai/bge-m3", []string{"Fast"}); !errors.Is(err, domain.ErrTagUnknown) {
		t.Errorf("an unknown tag: %v", err)
	}
	if got := actions(t, db); !slices.Contains(got, "ai.model_tags") || !slices.Contains(got, "ai.agent_update") {
		t.Errorf("console actions %v", got)
	}
}

// A provider in sync mode gets a lister and every model it lists, once;
// the next sync adds only the new ones.
func TestSyncProviders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, _, admin := settingsOn(t)
	f.listed["router"] = []string{"claude-haiku-5-5", "gpt-5"}
	p, err := s.CreateProvider(ctx, admin, service.NewProvider{ID: "router", Kind: "openai", APIKey: "sk-r", Mode: domain.ModeSync})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Models, []string{"claude-haiku-5-5", "gpt-5"}) || p.SyncedAt == nil || !p.SyncedAt.Equal(settingsNow) {
		t.Fatalf("after create %+v", p)
	}
	f.mu.Lock()
	f.listed["router"] = append(f.listed["router"], "gemini-3")
	f.mu.Unlock()
	if err = s.SyncProviders(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := s.Providers(ctx)
	if err != nil || !slices.Equal(list[0].Models, []string{"claude-haiku-5-5", "gemini-3", "gpt-5"}) {
		t.Fatalf("after sync %+v %v", list, err)
	}
	f.mu.Lock()
	catalog := 0
	for _, d := range f.deps {
		if d.InfoString("shopnexus") == "catalog" {
			catalog++
		}
	}
	f.mu.Unlock()
	if catalog != 3 {
		t.Errorf("%d catalog deployments, want 3", catalog)
	}
	if _, err = s.UpdateProvider(ctx, admin, "router", service.ProviderPatch{Mode: new(domain.ModePick)}); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.Providers(ctx); list[0].Mode != domain.ModePick || len(list[0].Models) != 3 {
		t.Errorf("back to pick %+v", list)
	}
}

// The router's settings, the budgets and the spend are LiteLLM's; a
// LiteLLM that is down is ai_gateway_unavailable.
func TestRotationBudgetsAndSpend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, db, admin := settingsOn(t)
	rot, err := s.UpdateRotation(ctx, admin, service.RotationPatch{NumRetries: new(3), CooldownS: new(30.0)})
	if err != nil || rot.NumRetries != 3 || rot.CooldownS != 30 || rot.RoutingStrategy != "simple-shuffle" {
		t.Fatalf("rotation %+v %v", rot, err)
	}
	if _, err = s.UpdateRotation(ctx, admin, service.RotationPatch{RoutingStrategy: new("random")}); err == nil {
		t.Error("an unknown strategy passed")
	}

	b, err := s.UpdateBudgets(ctx, admin, service.BudgetsPatch{
		MemberDailyUSD: &service.RoleBudgetsPatch{
			Moderator: service.Budget{Set: true, Value: new(2.0)}, Admin: service.Budget{Set: true, Value: new(5.0)},
		},
		Overrides:     &[]service.MemberDailyUSD{{MemberID: admin.MemberID, DailyUSD: 9}},
		MonthlyCapUSD: service.Budget{Set: true, Value: new(10.0)}, AlertPct: new(30), OnCap: new(service.OnCapStop),
	})
	if err != nil || *b.MemberDailyUSD.Moderator != 2 || *b.MemberDailyUSD.Admin != 5 || len(b.Overrides) != 1 ||
		*b.MonthlyCapUSD != 10 || b.AlertPct != 30 || b.OnCap != service.OnCapStop || b.Alerting {
		t.Fatalf("budgets %+v %v", b, err)
	}
	if k := f.key("agent"); k["end_user_budget_id"] != "console-member-moderator" {
		t.Errorf("the agent key's end user budget %v", k)
	}
	if got, want := f.customer(admin.MemberID.String()), "console-member-"+admin.MemberID.String(); got != want {
		t.Errorf("the admin's end user is on %q, want %q", got, want)
	}
	if got := f.customer(moderator.String()); got != "console-member-moderator" {
		t.Errorf("the moderator's end user is on %q", got)
	}
	b, err = s.UpdateBudgets(ctx, admin, service.BudgetsPatch{
		MemberDailyUSD: &service.RoleBudgetsPatch{Admin: service.Budget{Set: true, Value: new(0.0)}},
		Overrides:      &[]service.MemberDailyUSD{},
	})
	if err != nil || b.MemberDailyUSD.Admin != nil || len(b.Overrides) != 0 || f.customer(admin.MemberID.String()) != "console-member-admin" {
		t.Fatalf("clearing %+v %v", b, err)
	}
	i := slices.IndexFunc(b.Agents, func(a service.AgentBudget) bool { return a.AgentID == domain.ConsoleAssistant })
	j := slices.IndexFunc(b.Agents, func(a service.AgentBudget) bool { return a.AgentID == scamCheck })
	if i < 0 || j < 0 || b.Agents[i].SpentUSD != 1.25 || b.Agents[j].SpentUSD != 0.75 {
		t.Errorf("spent %+v", b.Agents)
	}

	f.mu.Lock()
	f.days = []map[string]any{
		{"date": "2026-11-18", "metrics": map[string]any{"spend": 3.0}, "breakdown": map[string]any{
			"model_groups": map[string]any{"console-agent": map[string]any{"metrics": map[string]any{"spend": 2.5}}},
		}},
		{"date": "2026-11-17", "metrics": map[string]any{"spend": 1.0}},
	}
	f.customerDays = []map[string]any{{"date": "2026-11-18", "metrics": map[string]any{"spend": 2.5}, "breakdown": map[string]any{
		"entities": map[string]any{moderator.String(): map[string]any{"metrics": map[string]any{"spend": 2.5, "api_requests": 6}}},
	}}}
	f.mu.Unlock()
	spend, err := s.Spend(ctx, admin, "7d")
	if err != nil || spend.TotalUSD != 4 || spend.Start != "2026-11-12" || len(spend.Days) != 2 || spend.Days[0].Date != "2026-11-17" ||
		len(spend.Days[1].Agents) != 1 || spend.Days[1].Agents[0].USD != 2.5 || len(spend.Members) != 1 ||
		spend.Members[0] != (service.MemberSpend{MemberID: moderator, DisplayName: "Lan", USD: 2.5, Requests: 3}) {
		t.Fatalf("spend %+v %v", spend, err)
	}
	// The month's 4 USD is past 30 % of the 10 USD cap, and a cap of 3 USD
	// with stop_agents refuses the console's turns.
	if b, err = s.Budgets(ctx); err != nil || !b.Alerting || b.Capped || b.MonthSpentUSD != 4 {
		t.Fatalf("alerting %+v %v", b, err)
	}
	if _, err = s.UpdateBudgets(ctx, admin, service.BudgetsPatch{MonthlyCapUSD: service.Budget{Set: true, Value: new(3.0)}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Ask(ctx, admin, uuid.NewV7(), service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrBudget) {
		t.Errorf("a question past the cap: %v", err)
	}
	if got := actions(t, db); !slices.Equal(got, []string{"ai.rotation", "ai.budgets", "ai.budgets", "ai.budgets"}) {
		t.Errorf("console actions %v", got)
	}

	f.mu.Lock()
	f.down = true
	f.mu.Unlock()
	if _, err = s.Providers(ctx); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Errorf("LiteLLM down: %v", err)
	}
}

// A provider lists the models its lister finds, each marked added when it
// is in its list; turning the provider off takes it out of the aliases
// unless an agent's model has no other provider, and turning it on puts
// it back. Its health is its share of LiteLLM's logged requests.
func TestProviderModelsEnabledAndHealth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, _, admin := settingsOn(t)
	f.listed["relay"] = []string{"claude-haiku-5-5", "gpt-5"}
	f.listed["backup"] = []string{"claude-haiku-5-5"}
	for _, p := range []string{"relay", "backup"} {
		if _, err := s.CreateProvider(ctx, admin, service.NewProvider{ID: p, Kind: "openai", APIKey: "sk-" + p}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AddProviderModel(ctx, admin, p, "claude-haiku-5-5", new(1.0), new(5.0)); err != nil {
			t.Fatal(err)
		}
	}
	models, err := s.ProviderModels(ctx, "relay")
	if err != nil || len(models) != 2 || models[0].Model != "claude-haiku-5-5" || !models[0].Added || models[1].Added ||
		*models[1].ContextTokens != 200000 || *models[0].InputCostPerMTok != 1 || *models[0].OutputCostPerMTok != 5 ||
		!slices.Contains(models[0].Tags, "Chat") {
		t.Fatalf("models %+v %v", models, err)
	}
	// gpt-5 is listed and not added: its price is the cost map's.
	if *models[1].InputCostPerMTok != 3 || *models[1].OutputCostPerMTok != 15 {
		t.Errorf("gpt-5's price %v %v", *models[1].InputCostPerMTok, *models[1].OutputCostPerMTok)
	}
	if _, err = s.ProviderModels(ctx, "nobody"); !errors.Is(err, domain.ErrProviderNotFound) {
		t.Errorf("an unknown provider: %v", err)
	}
	if _, err = s.UpdateAgent(ctx, admin, scamCheck, service.AgentPatch{Model: new("claude-haiku-5-5")}); err != nil {
		t.Fatal(err)
	}
	if got := f.aliases("scam-check"); len(got) != 2 {
		t.Fatalf("scam-check on both providers %v", got)
	}
	off, err := s.UpdateProvider(ctx, admin, "backup", service.ProviderPatch{Enabled: new(false)})
	if err != nil || off.Enabled {
		t.Fatalf("turning backup off %+v %v", off, err)
	}
	if got := f.aliases("scam-check"); len(got) != 1 || !strings.HasPrefix(got[0], "relay:") {
		t.Fatalf("scam-check with backup off %v", got)
	}
	if _, err = s.UpdateProvider(ctx, admin, "relay", service.ProviderPatch{Enabled: new(false)}); !errors.Is(err, domain.ErrProviderInUse) {
		t.Errorf("turning the last provider off: %v", err)
	}
	if _, err = s.UpdateProvider(ctx, admin, "backup", service.ProviderPatch{Enabled: new(true)}); err != nil {
		t.Fatal(err)
	}
	if got := f.aliases("scam-check"); len(got) != 2 {
		t.Fatalf("scam-check with backup on again %v", got)
	}

	f.mu.Lock()
	var relayDep, backupDep string
	for _, d := range f.deps {
		if d.ModelName == "scam-check" {
			if d.Params.Credential == "relay" {
				relayDep = d.ID()
			} else {
				backupDep = d.ID()
			}
		}
	}
	at := settingsNow.Add(-time.Hour).Format("2006-01-02T15:04:05.000000")
	f.logs = []map[string]any{
		{"model_id": relayDep, "status": "success", "request_duration_ms": 100, "startTime": at},
		{"model_id": relayDep, "status": "success", "request_duration_ms": 300, "startTime": at},
		{
			"model_id": relayDep, "status": "failure", "request_duration_ms": 50, "startTime": at,
			"metadata": map[string]any{"error_information": map[string]any{"error_message": "RateLimitError"}},
		},
		{"model_id": backupDep, "status": "success", "request_duration_ms": 200, "startTime": at},
	}
	f.mu.Unlock()
	health, err := s.ProviderHealth(ctx)
	if err != nil || len(health) != 2 {
		t.Fatalf("health %+v %v", health, err)
	}
	relay := health[slices.IndexFunc(health, func(h service.ProviderHealth) bool { return h.ID == "relay" })]
	if relay.Requests24h != 3 || relay.SharePct != 75 || relay.SuccessPct != 66.6 || relay.P50MS != 100 || relay.LastError != "RateLimitError" {
		t.Errorf("relay %+v", relay)
	}
}

// The usage of a console agent is its turns, an app agent's its alias's
// requests in LiteLLM; the tools and guardrails are the runs', each tool
// with its title.
func TestUsageToolsAndGuardrails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, _, admin := settingsOn(t)
	f.mu.Lock()
	f.days = []map[string]any{{"date": "2026-11-18", "metrics": map[string]any{"spend": 1.0}, "breakdown": map[string]any{
		"model_groups": map[string]any{"scam-check": map[string]any{"metrics": map[string]any{"spend": 1.0, "api_requests": 7}}},
	}}}
	f.mu.Unlock()
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

// A model's price is what LiteLLM keeps: the one in litellm_params, which
// the server writes, and none for a deployment without one, whose
// model_info LiteLLM shows as 0.
func TestModelPricesAreLiteLLMParams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _, _, admin := settingsOn(t)
	if _, err := s.CreateProvider(ctx, admin, service.NewProvider{ID: "relay", Kind: "openai", APIKey: "sk-relay"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProviderModel(ctx, admin, "relay", "priced", new(2.0), new(10.0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProviderModel(ctx, admin, "relay", "unpriced", nil, nil); err != nil {
		t.Fatal(err)
	}
	list, err := s.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list.Items {
		switch m.Model {
		case "priced":
			if m.InputCostPerMTok == nil || *m.InputCostPerMTok != 2 || *m.OutputCostPerMTok != 10 {
				t.Errorf("priced: %+v", m)
			}
		case "unpriced":
			if m.InputCostPerMTok != nil || m.OutputCostPerMTok != nil {
				t.Errorf("unpriced shows %v %v", m.InputCostPerMTok, m.OutputCostPerMTok)
			}
		default:
		}
	}
}
