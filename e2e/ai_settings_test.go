package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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
)

const settings = "/v1/console/settings"

type aiProvider struct {
	ID     string   `json:"id"`
	Weight float64  `json:"weight"`
	Mode   string   `json:"mode"`
	Models []string `json:"models"`
}

type aiAgent struct {
	ID                  string   `json:"id"`
	Kind                string   `json:"kind"`
	Name                string   `json:"name"`
	Alias               string   `json:"alias"`
	Model               string   `json:"model"`
	InstructionsVersion int      `json:"instructions_version"`
	MonthlyBudgetUSD    *float64 `json:"monthly_budget_usd"`
}

// An admin adds a provider and its models, tags a model, makes a console
// agent on it and changes agents, rotation and budgets; every change is
// in the audit log, and a moderator gets none of it.
func TestAISettings(t *testing.T) {
	t.Parallel()
	minh := signInMember(t, consoledomain.RoleAdmin)
	lan := signInMember(t, consoledomain.RoleModerator)
	signedOut := consoleSession{}
	suffix := newID()[24:]
	provider, model := "p-"+suffix, "e2e/model-"+suffix
	escaped := url.QueryEscape(model)

	made := expect[aiProvider](t, consoleCall(t, minh, "POST", settings+"/providers", map[string]any{
		"id": provider, "name": "Relay", "kind": "openai", "api_key": "sk-relay", "weight": 2, "order": 1,
	}), 201)
	if made.ID != provider || made.Weight != 2 || made.Mode != "pick" {
		t.Fatalf("provider %+v", made)
	}
	check(t, "the same id", consoleCall(t, minh, "POST", settings+"/providers", map[string]any{"id": provider, "kind": "openai", "api_key": "x"}),
		409, "ai_provider_exists")
	check(t, "a key LiteLLM refuses", consoleCall(t, minh, "POST", settings+"/providers",
		map[string]any{"id": "q-" + suffix, "kind": "openai", "api_key": gatewayRefused}), 422, "ai_gateway_refused")
	check(t, "a bad id", consoleCall(t, minh, "POST", settings+"/providers", map[string]any{"id": "Bad Id", "kind": "openai", "api_key": "x"}),
		400, "")
	list := expect[struct {
		Items []aiProvider `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/providers", nil), 200)
	if !slices.ContainsFunc(list.Items, func(p aiProvider) bool { return p.ID == provider }) {
		t.Fatalf("providers %+v", list)
	}

	added := expect[aiProvider](t, consoleCall(t, minh, "POST", settings+"/providers/"+provider+"/models",
		map[string]any{"model": model, "input_cost_per_mtok": 1, "output_cost_per_mtok": 5}), 200)
	if !slices.Contains(added.Models, model) {
		t.Fatalf("models %+v", added)
	}
	check(t, "a model LiteLLM refuses", consoleCall(t, minh, "POST", settings+"/providers/"+provider+"/models",
		map[string]any{"model": gatewayRefused}), 422, "ai_gateway_refused")
	check(t, "a model of an unknown provider", consoleCall(t, minh, "POST", settings+"/providers/nobody/models",
		map[string]any{"model": model}), 404, "ai_provider_not_found")
	check(t, "a model without its name", consoleCall(t, minh, "POST", settings+"/providers/"+provider+"/models", map[string]any{}), 400, "")
	expectStatus(t, consoleCall(t, minh, "POST", settings+"/providers/"+provider+"/models", map[string]any{"model": "spare-" + suffix}), 200)
	expectStatus(t, consoleCall(t, minh, "DELETE", settings+"/providers/"+provider+"/models?model=spare-"+suffix, nil), 200)
	check(t, "removing a model the provider lacks", consoleCall(t, minh, "DELETE", settings+"/providers/"+provider+"/models?model=none", nil),
		404, "ai_model_not_found")

	tags := expect[struct {
		Tags []string `json:"tags"`
	}](t, consoleCall(t, minh, "PUT", settings+"/models/tags", map[string]any{"model": model, "tags": []string{"Chat", "Tool use"}}), 200)
	if !slices.Equal(tags.Tags, []string{"Chat", "Tool use"}) {
		t.Fatalf("tags %+v", tags)
	}
	check(t, "an unknown tag", consoleCall(t, minh, "PUT", settings+"/models/tags", map[string]any{"model": model, "tags": []string{"Fast"}}),
		422, "ai_tag_unknown")
	check(t, "tags without a list", consoleCall(t, minh, "PUT", settings+"/models/tags", map[string]any{"model": model}), 400, "")
	models := expect[struct {
		Items []struct {
			Model string   `json:"model"`
			Tags  []string `json:"tags"`
		} `json:"items"`
		Tags []any `json:"tags"`
	}](t, consoleCall(t, minh, "GET", settings+"/models", nil), 200)
	if len(models.Tags) != 8 || !slices.ContainsFunc(models.Items, func(m struct {
		Model string   `json:"model"`
		Tags  []string `json:"tags"`
	},
	) bool {
		return m.Model == model && len(m.Tags) == 2
	}) {
		t.Fatalf("models %+v", models)
	}

	agentBody := map[string]any{
		"name": "Writer " + suffix, "alias": "writer-" + suffix, "model": model, "required_tags": []string{"Chat"},
		"instructions": "Write.", "tools": []string{"count"}, "limits": map[string]int{"deadline_s": 120}, "monthly_budget_usd": 5,
	}
	agent := expect[aiAgent](t, consoleCall(t, minh, "POST", settings+"/agents", agentBody), 201)
	if agent.Kind != "console" || agent.Model != model || *agent.MonthlyBudgetUSD != 5 {
		t.Fatalf("agent %+v", agent)
	}
	check(t, "the same alias", consoleCall(t, minh, "POST", settings+"/agents", agentBody), 409, "ai_agent_taken")
	check(t, "a model without the tags", consoleCall(t, minh, "POST", settings+"/agents", map[string]any{
		"name": "Seer " + suffix, "alias": "seer-" + suffix, "model": model, "required_tags": []string{"Vision"},
	}), 422, "ai_model_lacks_tags")
	check(t, "a model no provider has", consoleCall(t, minh, "POST", settings+"/agents", map[string]any{
		"name": "Ghost " + suffix, "alias": "ghost-" + suffix, "model": "nowhere-" + suffix,
	}), 404, "ai_model_not_found")
	check(t, "an agent without a model", consoleCall(t, minh, "POST", settings+"/agents", map[string]any{"name": "x", "alias": "xx"}), 400, "")

	changed := expect[aiAgent](t, consoleCall(t, minh, "PATCH", settings+"/agents/"+agent.ID,
		map[string]any{"instructions": "Write less.", "monthly_budget_usd": nil}), 200)
	if changed.InstructionsVersion != 2 || changed.MonthlyBudgetUSD != nil {
		t.Fatalf("changed %+v", changed)
	}
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
	agents := expect[struct {
		Items []aiAgent `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/agents", nil), 200)
	if !slices.ContainsFunc(agents.Items, func(a aiAgent) bool { return a.Alias == "scam-check" && a.Kind == "app" }) {
		t.Fatalf("agents %+v", agents)
	}

	available := expect[struct {
		Items []struct {
			Model string `json:"model"`
			Added bool   `json:"added"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/providers/"+provider+"/models", nil), 200)
	if len(available.Items) == 0 || !slices.ContainsFunc(available.Items, func(m struct {
		Model string `json:"model"`
		Added bool   `json:"added"`
	},
	) bool {
		return m.Model == model && m.Added
	}) {
		t.Errorf("available models %+v", available)
	}
	check(t, "the models of an unknown provider", consoleCall(t, minh, "GET", settings+"/providers/nobody/models", nil), 404,
		"ai_provider_not_found")
	expectStatus(t, consoleCall(t, minh, "GET", settings+"/providers/health", nil), 200)
	check(t, "turning off the only provider of a model", consoleCall(t, minh, "PATCH", settings+"/providers/"+provider,
		map[string]any{"enabled": false}), 409, "ai_provider_in_use")
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
		return u.AgentID == "0193e000-0000-7000-8000-00000000a101" && u.Requests == 2
	}) {
		t.Errorf("usage %+v", usage)
	}
	tools := expect[struct {
		Items []struct {
			DisplayName string `json:"display_name"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "GET", settings+"/tools?range=30d", nil), 200)
	if len(tools.Items) != 1 || tools.Items[0].DisplayName != "Ticket" {
		t.Errorf("tools %+v", tools)
	}
	expectStatus(t, consoleCall(t, minh, "GET", settings+"/guardrails", nil), 200)
	floors := expect[struct {
		Items []struct {
			App   string `json:"app"`
			Floor string `json:"floor"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "PATCH", settings+"/dashboards", map[string]any{
		"items": []map[string]string{{"app": "grafana", "floor": "moderator"}},
	}), 200)
	if len(floors.Items) != 6 || floors.Items[0].App != "grafana" {
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

	check(t, "deleting the only provider of a model", consoleCall(t, minh, "DELETE", settings+"/providers/"+provider, nil), 409,
		"ai_provider_in_use")
	check(t, "removing the only provider's model", consoleCall(t, minh, "DELETE", settings+"/providers/"+provider+"/models?model="+escaped, nil),
		409, "ai_provider_in_use")
	expectStatus(t, consoleCall(t, minh, "PATCH", settings+"/providers/"+provider, map[string]any{"weight": 3}), 200)
	check(t, "a key LiteLLM refuses", consoleCall(t, minh, "PATCH", settings+"/providers/"+provider, map[string]any{"api_key": gatewayRefused}),
		422, "ai_gateway_refused")
	check(t, "an unknown provider", consoleCall(t, minh, "PATCH", settings+"/providers/nobody", map[string]any{"weight": 1}), 404,
		"ai_provider_not_found")
	check(t, "an empty patch", consoleCall(t, minh, "PATCH", settings+"/providers/"+provider, map[string]any{}), 400, "")
	test := expect[struct {
		OK bool `json:"ok"`
	}](t, consoleCall(t, minh, "POST", settings+"/providers/"+provider+"/test", map[string]any{}), 200)
	if !test.OK {
		t.Errorf("test %+v", test)
	}
	check(t, "testing an unknown provider", consoleCall(t, minh, "POST", settings+"/providers/nobody/test", map[string]any{}), 404,
		"ai_provider_not_found")
	check(t, "a test with an unknown field", consoleCall(t, minh, "POST", settings+"/providers/"+provider+"/test",
		map[string]any{"fast": true}), 400, "")

	other := "o-" + suffix
	expectStatus(t, consoleCall(t, minh, "POST", settings+"/providers", map[string]any{"id": other, "kind": "anthropic", "api_key": "sk-o"}), 201)
	expectStatus(t, consoleCall(t, minh, "DELETE", settings+"/providers/"+other, nil), 204)
	check(t, "deleting an unknown provider", consoleCall(t, minh, "DELETE", settings+"/providers/"+other, nil), 404, "ai_provider_not_found")

	rot := expect[struct {
		NumRetries int `json:"num_retries"`
	}](t, consoleCall(t, minh, "PATCH", settings+"/rotation", map[string]any{"num_retries": 3}), 200)
	if rot.NumRetries != 3 {
		t.Errorf("rotation %+v", rot)
	}
	expectStatus(t, consoleCall(t, minh, "GET", settings+"/rotation", nil), 200)
	check(t, "an unknown strategy", consoleCall(t, minh, "PATCH", settings+"/rotation", map[string]any{"routing_strategy": "random"}), 400, "")

	budgets := expect[struct {
		MemberDailyUSD struct {
			Moderator float64 `json:"moderator"`
		} `json:"member_daily_usd"`
		Overrides []any  `json:"overrides"`
		OnCap     string `json:"on_cap"`
	}](t, consoleCall(t, minh, "PATCH", settings+"/budgets", map[string]any{
		"member_daily_usd": map[string]any{"moderator": 2, "admin": 0},
		"overrides":        []map[string]any{{"member_id": minh.ID, "daily_usd": 4}},
		"agents":           []map[string]any{{"agent_id": agent.ID, "monthly_budget_usd": 7}},
		"alert_pct":        90, "on_cap": "alert_only",
	}), 200)
	if budgets.MemberDailyUSD.Moderator != 2 || len(budgets.Overrides) != 1 || budgets.OnCap != "alert_only" {
		t.Errorf("budgets %+v", budgets)
	}
	expectStatus(t, consoleCall(t, minh, "GET", settings+"/budgets", nil), 200)
	check(t, "the budget of an unknown agent", consoleCall(t, minh, "PATCH", settings+"/budgets", map[string]any{
		"agents": []map[string]any{{"agent_id": newID(), "monthly_budget_usd": 1}},
	}), 404, "ai_agent_not_found")
	check(t, "a negative budget", consoleCall(t, minh, "PATCH", settings+"/budgets", map[string]any{"monthly_cap_usd": -1}), 400, "")
	check(t, "the budget of an unknown member", consoleCall(t, minh, "PATCH", settings+"/budgets", map[string]any{
		"overrides": []map[string]any{{"member_id": newID(), "daily_usd": 1}},
	}), 404, "member_not_found")
	spend := expect[struct {
		Range   string `json:"range"`
		Members []any  `json:"members"`
	}](t, consoleCall(t, minh, "GET", settings+"/spend?range=7d", nil), 200)
	if spend.Range != "7d" || spend.Members == nil {
		t.Errorf("spend %+v", spend)
	}

	actions := expect[struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}](t, consoleCall(t, minh, "GET", "/v1/console/actions?member_id="+minh.ID+"&limit=50", nil), 200)
	for _, want := range []string{
		"ai.provider_create", "ai.provider_models", "ai.model_tags", "ai.agent_create", "ai.agent_update", "ai.provider_update",
		"ai.provider_delete", "ai.rotation", "ai.budgets",
	} {
		if !slices.ContainsFunc(actions.Items, func(a struct {
			Action string `json:"action"`
		},
		) bool {
			return a.Action == want
		}) {
			t.Errorf("no %s in the audit log", want)
		}
	}

	for _, c := range settingsCalls(provider, escaped, agent.ID) {
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
func settingsCalls(provider, model, agent string) []settingsCall {
	p := settings + "/providers/" + provider
	return []settingsCall{
		{"GET", settings + "/providers", nil},
		{"POST", settings + "/providers", map[string]any{"id": "zz", "kind": "openai", "api_key": "x"}},
		{"PATCH", p, map[string]any{"weight": 1}},
		{"DELETE", p, nil},
		{"POST", p + "/models", map[string]any{"model": "m"}},
		{"DELETE", p + "/models?model=" + model, nil},
		{"POST", p + "/test", map[string]any{}},
		{"GET", settings + "/models", nil},
		{"PUT", settings + "/models/tags", map[string]any{"model": model, "tags": []string{}}},
		{"GET", settings + "/agents", nil},
		{"POST", settings + "/agents", map[string]any{"name": "x", "alias": "xx", "model": "m"}},
		{"PATCH", settings + "/agents/" + agent, map[string]any{"enabled": true}},
		{"GET", settings + "/agents/" + agent + "/versions", nil},
		{"GET", settings + "/rotation", nil},
		{"PATCH", settings + "/rotation", map[string]any{"num_retries": 1}},
		{"GET", settings + "/budgets", nil},
		{"PATCH", settings + "/budgets", map[string]any{"alert_pct": 50}},
		{"GET", settings + "/spend", nil},
		{"GET", settings + "/providers/health", nil},
		{"GET", p + "/models", nil},
		{"GET", settings + "/agents/usage", nil},
		{"GET", settings + "/tools", nil},
		{"GET", settings + "/guardrails", nil},
		{"GET", settings + "/dashboards", nil},
		{"PATCH", settings + "/dashboards", map[string]any{"items": []map[string]string{{"app": "grafana", "floor": "moderator"}}}},
	}
}

// A stack whose LiteLLM and agent are down answers ai_gateway_unavailable
// on every setting that reads or writes LiteLLM, and agent_unavailable on
// the skills. The second server has a database of its own, with an admin
// of its own.
func TestAISettingsDown(t *testing.T) {
	t.Parallel()
	cfg := serverConfig
	pool := pgtest.New(t)
	cfg.DatabaseURL, cfg.Restate = pool.Config().ConnString(), restatetest.Config(t)
	cfg.Agent.LiteLLMURL, cfg.Agent.BaseURL = "http://127.0.0.1:1", "http://127.0.0.1:1"
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
	for _, c := range settingsCalls("relay", "m", assistant) {
		switch {
		case c.path == settings+"/agents", strings.HasSuffix(c.path, "/tags"), strings.HasPrefix(c.path, settings+"/agents/0"),
			strings.HasSuffix(c.path, "/dashboards"):
			continue // the database's alone
		case strings.HasSuffix(c.path, "/tools"), strings.HasSuffix(c.path, "/guardrails"), strings.HasSuffix(c.path, "/usage"):
			check(t, c.method+" "+c.path, at(c.method, c.path, c.body), 503, "agent_unavailable")
		default:
			check(t, c.method+" "+c.path, at(c.method, c.path, c.body), 503, "ai_gateway_unavailable")
		}
	}
	check(t, "creating an agent", at("POST", settings+"/agents", map[string]any{"name": "Down", "alias": "down-agent", "model": "m"}), 503,
		"ai_gateway_unavailable")
	check(t, "a new model", at("PATCH", settings+"/agents/"+assistant, map[string]any{"model": "claude-haiku-5-5"}), 503,
		"ai_gateway_unavailable")
	check(t, "the skills", at("GET", "/v1/console/agent/skills", nil), 503, "agent_unavailable")
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
