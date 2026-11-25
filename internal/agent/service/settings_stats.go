package service

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
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
type ToolRecent = repository.RecentCall

// GuardrailStats is the AIGuardrailStats schema, the agent's own.
type GuardrailStats struct {
	Items  []GuardrailCount  `json:"items"`
	Recent []GuardrailNotice `json:"recent"`
}

// GuardrailCount is how often a rule acted.
type GuardrailCount = repository.RuleCount

// GuardrailNotice is one time a rule acted.
type GuardrailNotice = repository.RecentGuardrail

// turnStats counts the console turns of a range by agent and by member.
type turnStats struct {
	ByAgent  []agentTurns
	ByMember []repository.MemberCount
}

type agentTurns struct {
	AgentID  string
	Requests int
	Days     []DayCount
}

// rangeOf is the days of rng when Spend takes it, else of 7d.
func rangeOf(rng string) int {
	if days, ok := SpendRanges[rng]; ok {
		return days
	}
	return SpendRanges["7d"]
}

// recentStats is how many recent tool calls and guardrail events the stats
// list.
const recentStats = 20

// rangeDays is every UTC date of a range of n days ending today, oldest
// first, and the moment the first one starts.
func rangeDays(now time.Time, n int) (from time.Time, dates []string) {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	from = today.AddDate(0, 0, -(n - 1))
	dates = make([]string, 0, n)
	for i := range n {
		dates = append(dates, from.AddDate(0, 0, i).Format(time.DateOnly))
	}
	return from, dates
}

// byDay is, for each key of rows, its count on every date (0 where there
// was none).
func byDay(rows []repository.DayCount, dates []string) map[string][]int {
	at := map[string]int{}
	for i, d := range dates {
		at[d] = i
	}
	out := map[string][]int{}
	for _, r := range rows {
		key := ""
		if r.Key != nil {
			key = *r.Key
		}
		if out[key] == nil {
			out[key] = make([]int, len(dates))
		}
		if i, ok := at[r.Date]; ok {
			out[key][i] += r.Count
		}
	}
	return out
}

// turnsOf counts the console turns of the last n days by agent and by
// member.
func (s *Agent) turnsOf(ctx context.Context, n int) (turnStats, error) {
	from, dates := rangeDays(s.d.Now(), n)
	days, err := s.d.Runs.AgentDays(ctx, from)
	if err != nil {
		return turnStats{}, fmt.Errorf("count the turns by agent: %w", err)
	}
	members, err := s.d.Runs.MemberTurns(ctx, from)
	if err != nil {
		return turnStats{}, fmt.Errorf("count the turns by member: %w", err)
	}
	var out turnStats
	for agent, counts := range byDay(days, dates) {
		a := agentTurns{AgentID: agent, Days: make([]DayCount, 0, len(dates))}
		for i, d := range dates {
			a.Requests += counts[i]
			a.Days = append(a.Days, DayCount{Date: d, Requests: counts[i]})
		}
		out.ByAgent = append(out.ByAgent, a)
	}
	slices.SortFunc(out.ByAgent, func(a, b agentTurns) int {
		if a.Requests != b.Requests {
			return b.Requests - a.Requests
		}
		return strings.Compare(a.AgentID, b.AgentID)
	})
	out.ByMember = members
	return out, nil
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
	tags, err := s.d.Settings.ModelTags(ctx)
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
		// The catalog deployment's own price, which the admin set, wins
		// over the cost map's on the lister's row.
		own := role(d) == roleCatalog && (d.Params.InputCost != nil || d.Params.OutputCost != nil)
		if m.InputCostPerMTok == nil || own {
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

// priceOf is a deployment's price per million tokens. LiteLLM keeps a
// stored deployment's own price in litellm_params, where the server
// writes it, and drops one written in model_info alone; its model_info
// otherwise carries the price of its model cost map, or 0 or null for a
// model the map lacks. So the price is litellm_params', else model_info's
// when it is above 0, else none.
func priceOf(d litellm.Deployment) (in, out *float64) {
	if d.Params.InputCost != nil || d.Params.OutputCost != nil {
		return perMTok(d.Params.InputCost), perMTok(d.Params.OutputCost)
	}
	return perMTok(infoPositive(d.Info, "input_cost_per_token")), perMTok(infoPositive(d.Info, "output_cost_per_token"))
}

// contextOf is the input tokens LiteLLM's model_info gives the model, from
// its cost map.
func contextOf(d litellm.Deployment) *int {
	v := infoPositive(d.Info, "max_input_tokens")
	if v == nil {
		return nil
	}
	return new(int(*v))
}

// infoPositive is a number of model_info above 0, which LiteLLM sets to 0
// or null when its cost map lacks the model.
func infoPositive(m map[string]any, key string) *float64 {
	if v, ok := m[key].(float64); ok && v > 0 {
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
	if err := s.d.Gateway.AddDeployment(ctx, litellm.Deployment{
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
	now := s.d.Now()
	logs, err := s.d.Gateway.SpendLogs(ctx, now.Add(-24*time.Hour), now)
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
// agent's turns, an app agent's calls to its alias
// from LiteLLM.
func (s *Agent) AgentUsage(ctx context.Context, _ authn.ConsoleMember, rng string) ([]AgentUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	n := rangeOf(rng)
	agents, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the agents: %w", err)
	}
	turns, err := s.turnsOf(ctx, n)
	if err != nil {
		return nil, err
	}
	end := s.d.Now().UTC()
	acts, err := s.d.Gateway.DailyActivity(ctx, "/user", end.AddDate(0, 0, -(n-1)).Format(time.DateOnly), end.Format(time.DateOnly))
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
// range, from LiteLLM's end users, with their questions; members with
// neither are left out.
func (s *Agent) memberSpend(ctx context.Context, rng, start, end string) ([]MemberSpend, error) {
	acts, err := s.d.Gateway.DailyActivity(ctx, "/customer", start, end)
	if err != nil {
		return nil, gatewayError(err)
	}
	n := rangeOf(rng)
	turns, err := s.turnsOf(ctx, n)
	if err != nil {
		return nil, err
	}
	members, err := s.members(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MemberSpend, 0, len(members))
	for i := range members {
		m := &members[i]
		ms := MemberSpend{MemberID: m.ID, DisplayName: m.DisplayName}
		for _, act := range acts {
			ms.USD += act.ByEntity[m.ID.String()].Spend
		}
		for _, t := range turns.ByMember {
			if t.MemberID == m.ID {
				ms.Requests += t.Count
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

// Tools reads the agent's tool calls over a range: each tool's calls,
// failures, median time and calls by day, with its title, and the newest
// calls with their turn's member.
func (s *Agent) Tools(ctx context.Context, _ authn.ConsoleMember, rng string) (ToolStats, error) {
	n := rangeOf(rng)
	from, dates := rangeDays(s.d.Now(), n)
	totals, err := s.d.Runs.ToolTotals(ctx, from)
	if err != nil {
		return ToolStats{}, fmt.Errorf("count the tool calls: %w", err)
	}
	days, err := s.d.Runs.ToolDays(ctx, from)
	if err != nil {
		return ToolStats{}, fmt.Errorf("count the tool calls by day: %w", err)
	}
	recent, err := s.d.Runs.RecentCalls(ctx, from, recentStats)
	if err != nil {
		return ToolStats{}, fmt.Errorf("list the recent tool calls: %w", err)
	}
	daysOf := byDay(days, dates)
	out := ToolStats{Items: make([]ToolStat, 0, len(totals)), Recent: append(make([]ToolRecent, 0, len(recent)), recent...)}
	for _, t := range totals {
		item := ToolStat{Name: t.Name, DisplayName: domain.ToolTitles[t.Name], Calls: t.Calls, Failed: t.Failed, Days: []DayCalls{}}
		if item.DisplayName == "" {
			item.DisplayName = t.Name
		}
		if t.P50MS != nil {
			item.P50MS = new(int(math.Round(*t.P50MS)))
		}
		for i, d := range dates {
			calls := 0
			if c := daysOf[t.Name]; c != nil {
				calls = c[i]
			}
			item.Days = append(item.Days, DayCalls{Date: d, Calls: calls})
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// Guardrails reads the agent's guardrail events over a range: how often
// each rule acted, and the newest events.
func (s *Agent) Guardrails(ctx context.Context, _ authn.ConsoleMember, rng string) (GuardrailStats, error) {
	n := rangeOf(rng)
	from, _ := rangeDays(s.d.Now(), n)
	rules, err := s.d.Runs.GuardrailRules(ctx, from)
	if err != nil {
		return GuardrailStats{}, fmt.Errorf("count the guardrail events: %w", err)
	}
	recent, err := s.d.Runs.RecentGuardrails(ctx, from, recentStats)
	if err != nil {
		return GuardrailStats{}, fmt.Errorf("list the recent guardrail events: %w", err)
	}
	// The lists stay [] when empty.
	return GuardrailStats{
		Items:  append(make([]GuardrailCount, 0, len(rules)), rules...),
		Recent: append(make([]GuardrailNotice, 0, len(recent)), recent...),
	}, nil
}
