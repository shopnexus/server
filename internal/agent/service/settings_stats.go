package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/litellm"
)

// AvailableModel is the AIProviderModel schema: a model a provider lists,
// with its tags and price, and whether it is in the provider's list.
type AvailableModel struct {
	Model             string   `json:"model"`
	Tags              []string `json:"tags,omitempty"`
	InputCostPerMTok  *float64 `json:"input_cost_per_mtok,omitempty"`
	OutputCostPerMTok *float64 `json:"output_cost_per_mtok,omitempty"`
	ContextTokens     *int     `json:"context_tokens,omitempty"`
	Added             bool     `json:"added"`
}

// ProviderHealth is the AIProviderHealth schema: a provider's last 24
// hours as LiteLLM logged them.
type ProviderHealth struct {
	ID          string  `json:"id"`
	Requests24h int     `json:"requests_24h"`
	SharePct    float64 `json:"share_pct"`
	SuccessPct  float64 `json:"success_pct"`
	P50MS       int     `json:"p50_ms"`
	// CoolingUntil is the end of a cooldown; LiteLLM keeps cooldowns in
	// its router and shows none, so the server leaves it out.
	CoolingUntil *time.Time `json:"cooling_until,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

// AgentUsage is the AIAgentUsage schema: an agent's requests over a range,
// by day.
type AgentUsage struct {
	AgentID  uuid.UUID  `json:"agent_id"`
	Requests int        `json:"requests"`
	Days     []DayCount `json:"days"`
}

// DayCount is a day's requests.
type DayCount struct {
	Date     string `json:"date"`
	Requests int    `json:"requests"`
}

// MemberSpend is what a member spent through the console agents over a
// range, and how many questions they asked.
type MemberSpend struct {
	MemberID    uuid.UUID `json:"member_id"`
	DisplayName string    `json:"display_name"`
	USD         float64   `json:"usd"`
	Requests    int       `json:"requests"`
}

// ToolStats is the AIToolStats schema: the agent's tool calls over a range,
// each tool with the title the console shows.
type ToolStats struct {
	Items  []ToolStat   `json:"items"`
	Recent []ToolRecent `json:"recent"`
}

// ToolStat is one tool's calls.
type ToolStat struct {
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name"`
	Calls       int        `json:"calls"`
	Failed      int        `json:"failed"`
	P50MS       *int       `json:"p50_ms,omitempty"`
	Days        []DayCalls `json:"days"`
}

// DayCalls is a day's calls of a tool.
type DayCalls struct {
	Date  string `json:"date"`
	Calls int    `json:"calls"`
}

// ToolRecent is one recent tool call.
type ToolRecent struct {
	CallID     uuid.UUID  `json:"call_id"`
	TurnID     uuid.UUID  `json:"turn_id"`
	MemberID   *uuid.UUID `json:"member_id,omitempty"`
	Tool       string     `json:"tool"`
	Label      string     `json:"label"`
	DurationMS *int       `json:"duration_ms,omitempty"`
	Status     string     `json:"status"`
	At         time.Time  `json:"at"`
}

// GuardrailStats is the AIGuardrailStats schema, the agent's own.
type GuardrailStats struct {
	Items  []GuardrailCount  `json:"items"`
	Recent []GuardrailNotice `json:"recent"`
}

// GuardrailCount is how often a rule acted.
type GuardrailCount struct {
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

// GuardrailNotice is one time a rule acted.
type GuardrailNotice struct {
	Rule     string     `json:"rule"`
	Detail   string     `json:"detail"`
	MemberID *uuid.UUID `json:"member_id,omitempty"`
	TurnID   *uuid.UUID `json:"turn_id,omitempty"`
	At       time.Time  `json:"at"`
}

// turnStats is the agent's GET /stats/turns.
type turnStats struct {
	ByAgent []struct {
		AgentID  string     `json:"agent_id"`
		Requests int        `json:"requests"`
		Days     []DayCount `json:"days"`
	} `json:"by_agent"`
	ByMember []struct {
		MemberID uuid.UUID `json:"member_id"`
		Requests int       `json:"requests"`
	} `json:"by_member"`
}

// rangeOf is rng when Spend takes it, else 7d, and its days.
func rangeOf(rng string) (r string, days int) {
	if days, ok := SpendRanges[rng]; ok {
		return rng, days
	}
	return "7d", SpendRanges["7d"]
}

// stats reads one of the agent's /stats endpoints over rng.
func (s *Agent) stats(ctx context.Context, caller authn.ConsoleMember, what, rng string, out any) error {
	return s.send(ctx, caller, request{
		method: http.MethodGet, path: "/stats/" + what, query: url.Values{"range": {rng}}, out: out, timeout: readTimeout,
		late: domain.ErrUnavailable, notFound: domain.ErrUnavailable,
	})
}

// ProviderModels lists the models a provider lists, through its lister,
// and the ones in its list, each with whether it is added. A provider
// without a lister gets one first.
func (s *Agent) ProviderModels(ctx context.Context, id string) ([]AvailableModel, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return nil, err
	}
	c, ok := st.credential(id)
	if !ok {
		return nil, domain.ErrProviderNotFound
	}
	if st, err = s.ensureLister(ctx, st, st.providerOf(c)); err != nil {
		return nil, err
	}
	tags, err := s.settings.ModelTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the model tags: %w", err)
	}
	byModel := map[string]*AvailableModel{}
	for _, d := range st.deps {
		var model string
		switch r := role(d); {
		case r == roleCatalog && d.InfoString("provider") == id:
			model = d.InfoString("model")
		case r == roleLister && strings.HasPrefix(d.ModelName, id+"/") && d.ModelName != id+"/*":
			model = strings.TrimPrefix(d.ModelName, id+"/")
		default:
			continue
		}
		m, seen := byModel[model]
		if !seen {
			m = &AvailableModel{Model: model, Tags: tags[model]}
			byModel[model] = m
		}
		m.Added = m.Added || role(d) == roleCatalog
		if m.InputCostPerMTok == nil {
			m.InputCostPerMTok, m.OutputCostPerMTok = priceOf(d)
		}
		if m.ContextTokens == nil {
			m.ContextTokens = contextOf(d)
		}
	}
	out := make([]AvailableModel, 0, len(byModel))
	for _, m := range byModel {
		out = append(out, *m)
	}
	slices.SortFunc(out, func(a, b AvailableModel) int { return strings.Compare(a.Model, b.Model) })
	return out, nil
}

// priceOf is a deployment's price per million tokens, from its params or
// the model_info LiteLLM fills from its price list.
func priceOf(d litellm.Deployment) (in, out *float64) {
	in, out = perMTok(d.Params.InputCost), perMTok(d.Params.OutputCost)
	if in == nil {
		in, out = perMTok(infoNumber(d.Info, "input_cost_per_token")), perMTok(infoNumber(d.Info, "output_cost_per_token"))
	}
	return in, out
}

// contextOf is the input tokens LiteLLM's model_info gives the model.
func contextOf(d litellm.Deployment) *int {
	v := infoNumber(d.Info, "max_input_tokens")
	if v == nil || *v <= 0 {
		return nil
	}
	return new(int(*v))
}

func infoNumber(m map[string]any, key string) *float64 {
	if v, ok := m[key].(float64); ok {
		return &v
	}
	return nil
}

// ensureLister gives p its lister when it has none, and reads LiteLLM
// again so the lister's models are in the state.
func (s *Agent) ensureLister(ctx context.Context, st state, p Provider) (state, error) {
	lister := p.ID + "/*"
	if slices.ContainsFunc(st.deps, func(d litellm.Deployment) bool { return d.ModelName == lister }) {
		return st, nil
	}
	if err := s.gateway.AddDeployment(ctx, litellm.Deployment{
		ModelName: lister,
		Params:    litellm.Params{Model: route(p.Kind, "*"), Credential: p.ID},
		Info:      map[string]any{roleKey: roleLister, "provider": p.ID},
	}); err != nil {
		return state{}, gatewayError(err)
	}
	return s.readState(ctx)
}

// ProviderHealth counts each provider's requests of the last 24 hours in
// LiteLLM's logs, by the deployment that served them: their share of all,
// the share that succeeded, the median duration and the last error.
func (s *Agent) ProviderHealth(ctx context.Context) ([]ProviderHealth, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	logs, err := s.gateway.SpendLogs(ctx, now.Add(-24*time.Hour), now)
	if err != nil {
		return nil, gatewayError(err)
	}
	providerOf := map[string]string{}
	for _, d := range st.deps {
		if p := d.Params.Credential; p != "" && d.ID() != "" {
			providerOf[d.ID()] = p
		}
	}
	type tally struct {
		n, ok     int
		durations []int
		lastErr   string
		lastAt    time.Time
	}
	tallies := map[string]*tally{}
	total := 0
	for _, l := range logs {
		p, known := providerOf[l.ModelID]
		if !known {
			continue
		}
		t := tallies[p]
		if t == nil {
			t = &tally{}
			tallies[p] = t
		}
		total++
		t.n++
		t.durations = append(t.durations, l.DurationMS)
		if l.Status == "" || l.Status == "success" {
			t.ok++
		} else if l.Start.After(t.lastAt) || t.lastErr == "" {
			t.lastErr, t.lastAt = l.Error, l.Start
			if t.lastErr == "" {
				t.lastErr = l.Status
			}
		}
	}
	out := make([]ProviderHealth, 0, len(st.creds))
	for _, c := range st.creds {
		h := ProviderHealth{ID: c.Name}
		if t := tallies[c.Name]; t != nil {
			h.Requests24h, h.LastError = t.n, t.lastErr
			h.SharePct = pct(t.n, total)
			h.SuccessPct = pct(t.ok, t.n)
			slices.Sort(t.durations)
			h.P50MS = t.durations[len(t.durations)/2]
		}
		out = append(out, h)
	}
	return out, nil
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part*1000/whole) / 10
}

// AgentUsage counts each agent's requests by day over a range: a console
// agent's turns from the agent service, an app agent's calls to its alias
// from LiteLLM.
func (s *Agent) AgentUsage(ctx context.Context, caller authn.ConsoleMember, rng string) ([]AgentUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	rng, n := rangeOf(rng)
	agents, err := s.settings.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the agents: %w", err)
	}
	var turns turnStats
	if err = s.stats(ctx, caller, "turns", rng, &turns); err != nil {
		return nil, err
	}
	end := s.now().UTC()
	acts, err := s.gateway.DailyActivity(ctx, "/user", end.AddDate(0, 0, -(n-1)).Format(time.DateOnly), end.Format(time.DateOnly))
	if err != nil {
		return nil, gatewayError(err)
	}
	slices.SortFunc(acts, func(a, b litellm.Activity) int { return strings.Compare(a.Date, b.Date) })
	out := make([]AgentUsage, 0, len(agents))
	for i := range agents {
		a := &agents[i]
		u := AgentUsage{AgentID: a.ID, Days: []DayCount{}}
		if a.Kind == domain.AgentConsole {
			for _, t := range turns.ByAgent {
				if t.AgentID == a.ID.String() || t.AgentID == a.Alias {
					u.Requests += t.Requests
					u.Days = append(u.Days, t.Days...)
				}
			}
		} else {
			for _, act := range acts {
				if m, ok := act.ByModel[a.Alias]; ok {
					u.Requests += m.Requests
					u.Days = append(u.Days, DayCount{Date: act.Date, Requests: m.Requests})
				}
			}
		}
		out = append(out, u)
	}
	return out, nil
}

// memberSpend is each member's spend through the console agents over a
// range, from LiteLLM's end users, with their questions from the agent
// service; members with neither are left out.
func (s *Agent) memberSpend(ctx context.Context, caller authn.ConsoleMember, rng, start, end string) ([]MemberSpend, error) {
	acts, err := s.gateway.DailyActivity(ctx, "/customer", start, end)
	if err != nil {
		return nil, gatewayError(err)
	}
	var turns turnStats
	if err = s.stats(ctx, caller, "turns", rng, &turns); err != nil {
		return nil, err
	}
	members, err := s.members(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MemberSpend, 0, len(members))
	for _, m := range members {
		ms := MemberSpend{MemberID: m.ID, DisplayName: m.DisplayName}
		for _, act := range acts {
			ms.USD += act.ByEntity[m.ID.String()].Spend
		}
		for _, t := range turns.ByMember {
			if t.MemberID == m.ID {
				ms.Requests += t.Requests
			}
		}
		if ms.USD > 0 || ms.Requests > 0 {
			out = append(out, ms)
		}
	}
	slices.SortFunc(out, func(a, b MemberSpend) int {
		switch {
		case a.USD > b.USD:
			return -1
		case a.USD < b.USD:
			return 1
		default:
			return 0
		}
	})
	return out, nil
}

// Tools reads the agent's tool calls over a range, each tool with its
// title.
func (s *Agent) Tools(ctx context.Context, caller authn.ConsoleMember, rng string) (ToolStats, error) {
	rng, _ = rangeOf(rng)
	var out ToolStats
	if err := s.stats(ctx, caller, "tools", rng, &out); err != nil {
		return ToolStats{}, err
	}
	for i := range out.Items {
		out.Items[i].DisplayName = domain.ToolTitles[out.Items[i].Name]
		if out.Items[i].DisplayName == "" {
			out.Items[i].DisplayName = out.Items[i].Name
		}
		if out.Items[i].Days == nil {
			out.Items[i].Days = []DayCalls{}
		}
	}
	if out.Items == nil {
		out.Items = []ToolStat{}
	}
	if out.Recent == nil {
		out.Recent = []ToolRecent{}
	}
	return out, nil
}

// Guardrails reads the agent's guardrail notices over a range.
func (s *Agent) Guardrails(ctx context.Context, caller authn.ConsoleMember, rng string) (GuardrailStats, error) {
	rng, _ = rangeOf(rng)
	var out GuardrailStats
	if err := s.stats(ctx, caller, "guardrails", rng, &out); err != nil {
		return GuardrailStats{}, err
	}
	if out.Items == nil {
		out.Items = []GuardrailCount{}
	}
	if out.Recent == nil {
		out.Recent = []GuardrailNotice{}
	}
	return out, nil
}
