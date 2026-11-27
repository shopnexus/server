package e2e

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/app"
	"github.com/shopnexus/server/internal/console"
	consoledomain "github.com/shopnexus/server/internal/console/domain"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/provider/bifrost"
)

const settings = "/v1/console/settings"

type aiModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model_id"`
}

type aiCaller struct {
	Name           string `json:"name"`
	RequestsPerMin *int   `json:"requests_per_min"`
	TokensPerMin   *int   `json:"tokens_per_min"`
}

type aiAgent struct {
	ID                  string       `json:"id"`
	Kind                string       `json:"kind"`
	Name                string       `json:"name"`
	Slug                string       `json:"slug"`
	Model               aiModelRef   `json:"model"`
	Fallbacks           []aiModelRef `json:"fallbacks"`
	InstructionsVersion int          `json:"instructions_version"`
	MonthlyBudgetUSD    *float64     `json:"monthly_budget_usd"`
	BudgetSpentUSD      *float64     `json:"budget_spent_usd"`
	BudgetResetsAt      *time.Time   `json:"budget_resets_at"`
	Callers             []aiCaller   `json:"callers"`
}

type aiPrice struct {
	Input     float64  `json:"input"`
	Output    float64  `json:"output"`
	CacheRead *float64 `json:"cache_read"`
}

type aiModel struct {
	Provider     string     `json:"provider"`
	Model        string     `json:"model_id"`
	Tags         []string   `json:"tags"`
	Price        *aiPrice   `json:"price"`
	CatalogPrice *aiPrice   `json:"catalog_price"`
	Agents       []struct{} `json:"agents"`
}

var (
	haiku  = aiModelRef{"upstream", "claude-haiku-5-5"}
	gemini = aiModelRef{"openrouter", "google/gemini-2.5-flash"}
)

// An admin reads the providers and catalogs, prices and tags a model,
// makes a console agent on it and changes it; Bifrost takes each change,
// every change is in the audit log, and a moderator gets none of it.
// UC-MOD-12 A1, A3, E1, E2, E3, E5, E6.
func TestAISettings(t *testing.T) {
	t.Parallel()
	minh := signInMember(t, consoledomain.RoleAdmin)
	lan := signInMember(t, consoledomain.RoleModerator)
	signedOut := consoleSession{}
	suffix := newID()[24:]

	gateway.SetStats("openrouter", 40, 90)
	providers := expect[struct {
		Items []struct {
			Name       string `json:"name"`
			Kind       string `json:"kind"`
			Status     string `json:"status"`
			ModelCount int    `json:"model_count"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/providers", nil), 200)
	if len(providers.Items) != 2 || providers.Items[0].Name != "openrouter" || providers.Items[0].Status != "degraded" ||
		providers.Items[0].ModelCount != 3 || providers.Items[1].Kind != "custom" || providers.Items[1].Status != "unknown" {
		t.Errorf("providers %+v", providers)
	}
	catalog := expect[struct {
		Items []struct {
			Model        string   `json:"model_id"`
			CatalogPrice *aiPrice `json:"catalog_price"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/providers/openrouter/models", nil), 200)
	if len(catalog.Items) != 3 || catalog.Items[0].Model != "baai/bge-m3" || catalog.Items[0].CatalogPrice.Input != 1e-8 {
		t.Errorf("openrouter's catalog %+v", catalog)
	}
	check(t, "the catalog of an unknown provider", consoleCall(t, minh, "GET", settings+"/providers/nobody/models", nil), 404,
		"ai_provider_not_found")

	// A model with no price yet gets one; an unknown provider's is refused.
	qwen := map[string]any{"provider": "openrouter", "model_id": "qwen/qwen3-embedding-8b"}
	priced := maps.Clone(qwen)
	priced["input"], priced["output"] = 2e-8, 0
	if got := expect[aiPrice](t, consoleCall(t, minh, "PUT", settings+"/models/price", priced), 200); got.Input != 2e-8 || got.CacheRead != nil {
		t.Errorf("qwen's price %+v", got)
	}
	if o, ok := gateway.Overrides()["server:openrouter/qwen/qwen3-embedding-8b"]; !ok || o == nil {
		t.Errorf("Bifrost's overrides %v", gateway.Overrides())
	}
	check(t, "a negative price", consoleCall(t, minh, "PUT", settings+"/models/price",
		map[string]any{"provider": "openrouter", "model_id": "x", "input": -1, "output": 0}), 422, "ai_price_invalid")
	check(t, "an unknown provider's price", consoleCall(t, minh, "PUT", settings+"/models/price",
		map[string]any{"provider": "nobody", "model_id": "x", "input": 1, "output": 1}), 404, "ai_provider_not_found")
	check(t, "a price without output", consoleCall(t, minh, "PUT", settings+"/models/price", qwen), 400, "")

	tagged := maps.Clone(qwen)
	tagged["tags"] = []string{"Multilingual", "Embedding"}
	if got := expect[struct {
		Tags []string `json:"tags"`
	}](t, consoleCall(t, minh, "PUT", settings+"/models/tags", tagged), 200); !slices.Equal(got.Tags, []string{"Embedding", "Multilingual"}) {
		t.Errorf("qwen's tags %+v", got)
	}
	tagged["tags"] = []string{"Fast"}
	check(t, "an unknown tag", consoleCall(t, minh, "PUT", settings+"/models/tags", tagged), 422, "ai_tag_unknown")
	check(t, "tags without a list", consoleCall(t, minh, "PUT", settings+"/models/tags", qwen), 400, "")

	models := expect[struct {
		Items []aiModel `json:"items"`
		Tags  []any     `json:"tags"`
	}](t, consoleCall(t, minh, "GET", settings+"/models", nil), 200)
	byName := map[string]aiModel{}
	for _, m := range models.Items {
		byName[m.Provider+"/"+m.Model] = m
	}
	if h := byName["upstream/claude-haiku-5-5"]; len(models.Tags) != 8 || h.Price == nil || h.CatalogPrice != nil || len(h.Agents) == 0 {
		t.Errorf("haiku %+v", h)
	}
	if q := byName["openrouter/qwen/qwen3-embedding-8b"]; q.Price == nil || q.CatalogPrice == nil || len(q.Tags) != 2 || len(q.Agents) != 0 {
		t.Errorf("qwen %+v", q)
	}

	slug := "writer-" + suffix
	agentBody := map[string]any{
		"name": "Writer " + suffix, "slug": slug, "model": haiku, "fallbacks": []aiModelRef{gemini}, "required_tags": []string{"Chat"},
		"instructions": "Write.", "capabilities": []string{"tickets.read"}, "limits": map[string]int{"deadline_s": 120}, "monthly_budget_usd": 5,
	}
	agent := expect[aiAgent](t, consoleCall(t, minh, "POST", settings+"/agents", agentBody), 201)
	if agent.Kind != "console" || agent.Model != haiku || len(agent.Fallbacks) != 1 || *agent.MonthlyBudgetUSD != 5 ||
		len(agent.Callers) != 1 || agent.Callers[0].Name != slug {
		t.Fatalf("agent %+v", agent)
	}
	key, ok := gateway.Keys()[slug]
	if !ok || key.Metadata[bifrost.MetaModel] != "upstream/claude-haiku-5-5" || key.Metadata[bifrost.MetaFallbacks] != "openrouter/google/gemini-2.5-flash" {
		t.Errorf("the writer's key %+v", key)
	}
	if team := gateway.Teams()[slug]; len(team.Budgets) != 1 || team.Budgets[0].MaxLimit != 5 {
		t.Errorf("the writer's team %+v", team)
	}
	// The same request again answers the agent it made; another name on the slug is refused.
	if again := expect[aiAgent](t, consoleCall(t, minh, "POST", settings+"/agents", agentBody), 201); again.ID != agent.ID {
		t.Fatalf("the same request: %+v", again)
	}
	taken := maps.Clone(agentBody)
	taken["name"] = "Other " + suffix
	check(t, "the same slug", consoleCall(t, minh, "POST", settings+"/agents", taken), 409, "ai_agent_taken")
	check(t, "a model without the tags", consoleCall(t, minh, "POST", settings+"/agents", map[string]any{
		"name": "Seer " + suffix, "slug": "seer-" + suffix, "model": haiku, "required_tags": []string{"Reasoning"},
	}), 422, "ai_model_lacks_tags")
	check(t, "a model with no price", consoleCall(t, minh, "POST", settings+"/agents", map[string]any{
		"name": "Ghost " + suffix, "slug": "ghost-" + suffix, "model": aiModelRef{"upstream", "nowhere-" + suffix},
	}), 422, "ai_model_unpriced")
	check(t, "an agent without a model", consoleCall(t, minh, "POST", settings+"/agents", map[string]any{"name": "x", "slug": "xx"}), 400, "")

	changed := expect[aiAgent](t, consoleCall(t, minh, "PATCH", settings+"/agents/"+agent.ID,
		map[string]any{"instructions": "Write less.", "monthly_budget_usd": nil}), 200)
	if changed.InstructionsVersion != 2 || changed.MonthlyBudgetUSD != nil {
		t.Fatalf("changed %+v", changed)
	}
	limited := expect[aiAgent](t, consoleCall(t, minh, "PATCH", settings+"/agents/"+agent.ID,
		map[string]any{"callers": []map[string]any{{"name": slug, "requests_per_min": 30, "tokens_per_min": nil}}, "monthly_budget_usd": 8}), 200)
	if *limited.MonthlyBudgetUSD != 8 || len(limited.Callers) != 1 || *limited.Callers[0].RequestsPerMin != 30 || *gateway.Keys()[slug].RateLimit.RequestMaxLimit != 30 {
		t.Errorf("the writer's callers %+v", limited.Callers)
	}
	check(t, "a caller not the agent's", consoleCall(t, minh, "PATCH", settings+"/agents/"+agent.ID,
		map[string]any{"callers": []map[string]any{{"name": "server-trust", "requests_per_min": 1, "tokens_per_min": nil}}}),
		422, "ai_agent_invalid")
	check(t, "another agent's name", consoleCall(t, minh, "PATCH", settings+"/agents/"+agent.ID, map[string]any{"name": "Console assistant"}),
		409, "ai_agent_taken")
	check(t, "an app agent's name", consoleCall(t, minh, "PATCH", settings+"/agents/0193e000-0000-7000-8000-00000000a103",
		map[string]any{"name": "Photos"}), 422, "ai_agent_app_fixed")
	check(t, "an unknown agent", consoleCall(t, minh, "PATCH", settings+"/agents/"+newID(), map[string]any{"enabled": false}), 404,
		"ai_agent_not_found")
	check(t, "an empty patch", consoleCall(t, minh, "PATCH", settings+"/agents/"+agent.ID, map[string]any{}), 400, "")
	versions := expect[struct {
		Items []struct {
			Version int `json:"version"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/agents/"+agent.ID+"/versions", nil), 200)
	if len(versions.Items) != 2 || versions.Items[0].Version != 2 {
		t.Fatalf("versions %+v", versions)
	}
	check(t, "the versions of an unknown agent", consoleCall(t, minh, "GET", settings+"/agents/"+newID()+"/versions", nil), 404,
		"ai_agent_not_found")

	gateway.SetSpent(slug, 1.25)
	agents := expect[struct {
		Items []aiAgent `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/agents", nil), 200)
	i := slices.IndexFunc(agents.Items, func(a aiAgent) bool { return a.ID == agent.ID })
	now := time.Now().UTC()
	resets := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	if i < 0 || *agents.Items[i].BudgetSpentUSD != 1.25 || !agents.Items[i].BudgetResetsAt.Equal(resets) ||
		!slices.ContainsFunc(agents.Items, func(a aiAgent) bool { return a.Slug == "scam-check" && a.Kind == "app" }) {
		t.Fatalf("agents %+v", agents)
	}

	askAndWait(t, minh, "Tuần này thế nào?")
	usage := expect[struct {
		Items []struct {
			AgentID  string `json:"agent_id"`
			Requests int    `json:"requests"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/agents/usage?range=7d", nil), 200)
	if !slices.ContainsFunc(usage.Items, func(u struct {
		AgentID  string `json:"agent_id"`
		Requests int    `json:"requests"`
	},
	) bool {
		return u.AgentID == "0193e000-0000-7000-8000-00000000a101" && u.Requests >= 1
	}) {
		t.Errorf("usage %+v", usage)
	}

	gateway.Log(now, slug, 0.5, 2)
	today := now.Format(time.DateOnly)
	spend := expect[struct {
		Days []struct {
			Date    string             `json:"date"`
			ByAgent map[string]float64 `json:"by_agent"`
		} `json:"days"`
		LagS int `json:"lag_s"`
	}](t, consoleCall(t, minh, "GET", settings+"/spend?from="+now.AddDate(0, 0, -6).Format(time.DateOnly)+"&to="+today, nil), 200)
	if len(spend.Days) != 7 || spend.Days[6].Date != today || spend.Days[6].ByAgent[slug] != 0.5 || spend.LagS != 60 {
		t.Errorf("spend %+v", spend)
	}
	check(t, "a range that ends before it starts", consoleCall(t, minh, "GET", settings+"/spend?from="+today+"&to=2020-01-01", nil),
		422, "ai_spend_range_invalid")
	check(t, "a range without its end", consoleCall(t, minh, "GET", settings+"/spend?from="+today, nil), 400, "")

	floors := expect[struct {
		Items []struct {
			App   string `json:"app"`
			Floor string `json:"floor"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "PATCH", settings+"/dashboards", map[string]any{
		"items": []map[string]string{{"app": "grafana", "floor": "moderator"}},
	}), 200)
	if len(floors.Items) != 6 || floors.Items[0].App != "grafana" || !slices.ContainsFunc(floors.Items, func(f struct {
		App   string `json:"app"`
		Floor string `json:"floor"`
	},
	) bool {
		return f.App == "bifrost"
	}) {
		t.Errorf("floors %+v", floors)
	}
	expectStatus(t, consoleCall(t, minh, "GET", settings+"/dashboards", nil), 200)
	check(t, "an unknown dashboard", consoleCall(t, minh, "PATCH", settings+"/dashboards", map[string]any{
		"items": []map[string]string{{"app": "kibana", "floor": "admin"}},
	}), 400, "")
	me := expect[struct {
		Dashboards []any `json:"dashboards"`
	}](t, consoleCall(t, lan, "GET", "/v1/console/me", nil), 200)
	if len(me.Dashboards) != 6 {
		t.Errorf("a moderator's dashboards %+v", me)
	}

	actions := expect[struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "GET", "/v1/console/actions?member_id="+minh.ID+"&limit=50", nil), 200)
	for _, want := range []string{"ai.model_price", "ai.model_tags", "ai.agent_create", "ai.agent_update"} {
		if !slices.ContainsFunc(actions.Items, func(a struct {
			Action string `json:"action"`
		},
		) bool {
			return a.Action == want
		}) {
			t.Errorf("no %s in the audit log", want)
		}
	}

	for _, c := range settingsCalls(agent.ID) {
		check(t, c.method+" "+c.path+" as a moderator", consoleCall(t, lan, c.method, c.path, c.body), 403, "console_forbidden")
		expectStatus(t, consoleCall(t, signedOut, c.method, c.path, c.body), 401)
	}
}

type settingsCall struct {
	method, path string
	body         any
}

// settingsCalls is one call of each settings operation, with a body its
// schema takes, so the guard answers rather than the validator.
func settingsCalls(agent string) []settingsCall {
	model := map[string]any{"provider": "openrouter", "model_id": "baai/bge-m3"}
	return []settingsCall{
		{"GET", settings + "/providers", nil},
		{"GET", settings + "/providers/openrouter/models", nil},
		{"GET", settings + "/models", nil},
		{"PUT", settings + "/models/tags", map[string]any{"provider": "openrouter", "model_id": "baai/bge-m3", "tags": []string{"Embedding"}}},
		{"PUT", settings + "/models/price", map[string]any{"provider": "openrouter", "model_id": "baai/bge-m3", "input": 1e-8, "output": 0}},
		{"GET", settings + "/agents", nil},
		{"POST", settings + "/agents", map[string]any{"name": "x", "slug": "xx", "model": model}},
		{"PATCH", settings + "/agents/" + agent, map[string]any{"enabled": true}},
		{"GET", settings + "/agents/" + agent + "/versions", nil},
		{"GET", settings + "/spend?from=2026-11-01&to=2026-11-07", nil},
		{"GET", settings + "/agents/usage", nil},
		{"GET", settings + "/dashboards", nil},
		{"PATCH", settings + "/dashboards", map[string]any{"items": []map[string]string{{"app": "grafana", "floor": "moderator"}}}},
	}
}

// A stack whose Bifrost and agent are down answers ai_gateway_unavailable
// on every setting that reads or writes Bifrost, with the change saved,
// and agent_unavailable on a question; the agents without their spend,
// the tags and versions are the server's. The
// second server has a database and an admin of its own.
// UC-MOD-12 A2.
func TestAISettingsDown(t *testing.T) {
	t.Parallel()
	cfg := serverConfig
	pool := pgtest.New(t)
	down := newGateway()
	t.Cleanup(down.Close)
	down.SetDown(true)
	cfg.DatabaseURL, cfg.Restate = pool.Config().ConnString(), restatetest.Config(t)
	cfg.Gateway.URL, cfg.Agent.RuntimeURL = down.URL(), "http://127.0.0.1:1"
	a, err := app.New(context.Background(), cfg, discard,
		app.WithPushSender(phones), app.WithEmailSender(mails), app.WithPasswordHasher(consoledomain.Fast), fakeAI())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	keys, err := consolesvc.NewKeys(cfg.Console, consoledomain.Fast)
	if err != nil {
		t.Fatal(err)
	}
	svc := console.NewService(pool, console.Settings{Keys: keys, Config: cfg.Console}, nil, time.Now)
	admin := signInMemberAt(t, srv.URL, svc, consoledomain.RoleAdmin)
	at := func(method, path string, body any) Response {
		return callAt(t, srv.URL, method, path, body, [2]string{"X-Requested-With", "shopnexus-console"},
			[2]string{"Cookie", "console_session=" + admin})
	}
	assistant := "0193e000-0000-7000-8000-00000000a101"
	for _, c := range settingsCalls(assistant) {
		switch {
		case c.method == "POST", c.method == "PATCH" && strings.Contains(c.path, "/agents/"), strings.HasSuffix(c.path, "/price"),
			strings.Contains(c.path, "/providers"), strings.HasSuffix(c.path, "/models"), strings.Contains(c.path, "/spend"),
			strings.Contains(c.path, "/usage"):
			check(t, c.method+" "+c.path, at(c.method, c.path, c.body), 503, "ai_gateway_unavailable")
		default:
			expectStatus(t, at(c.method, c.path, c.body), 200)
		}
	}
	// The change Bifrost could not take is saved.
	agents := expect[struct {
		Items []aiAgent `json:"items"`
	}](t, at("GET", settings+"/agents", nil), 200)
	if i := slices.IndexFunc(agents.Items, func(a aiAgent) bool { return a.Slug == "xx" }); i < 0 || agents.Items[i].BudgetSpentUSD != nil ||
		agents.Items[i].BudgetResetsAt != nil {
		t.Errorf("agents %+v", agents)
	}
	own := expect[agentConversation](t, at("POST", "/v1/console/agent/conversations", map[string]any{}), 200)
	check(t, "a question", at("POST", "/v1/console/agent/conversations/"+own.ID+"/turns", map[string]string{"text": "?"}), 503,
		"agent_unavailable")
}

// signInMemberAt creates a member in svc's database and signs it in at
// base, returning the session cookie's value.
func signInMemberAt(t *testing.T, base string, svc *consolesvc.Console, role consoledomain.Role) string {
	t.Helper()
	email := newMemberEmail()
	_, temp, err := svc.CreateMember(context.Background(), consolesvc.CreateMemberInput{
		Email: email, DisplayName: "E2E Admin", Role: role, By: audit.ByCLI("e2e"),
	})
	if err != nil {
		t.Fatal(err)
	}
	csrf := [2]string{"X-Requested-With", "shopnexus-console"}
	expectStatus(t, callAt(t, base, "POST", "/v1/console/login", map[string]string{"email": email, "password": temp, "new_password": memberPassword},
		csrf), 428)
	res := callAt(t, base, "POST", "/v1/console/login/totp-enrollment", map[string]string{"email": email, "password": memberPassword}, csrf)
	secret := decodeBase32(t, expect[struct {
		Secret string `json:"secret"`
	}](t, res, http.StatusOK).Secret)
	res = callAt(t, base, "POST", "/v1/console/login", map[string]string{
		"email": email, "password": memberPassword, "code": consoledomain.Code(secret, time.Now()),
	}, csrf)
	expectStatus(t, res, http.StatusOK)
	return sessionCookieOf(t, res).Value
}
