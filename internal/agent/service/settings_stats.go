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
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/bifrost"
)

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

// turnStats counts the console turns of a range by agent.
type turnStats struct {
	ByAgent []agentTurns
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

// turnsOf counts the console turns of the last n days by agent.
func (s *Agent) turnsOf(ctx context.Context, n int) (turnStats, error) {
	from, dates := rangeDays(s.d.Now(), n)
	days, err := s.d.Runs.AgentDays(ctx, from)
	if err != nil {
		return turnStats{}, fmt.Errorf("count the turns by agent: %w", err)
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
	return out, nil
}

// AgentUsage counts each agent's requests by day over a range: a console
// agent's turns, an app agent's calls through Bifrost, by its team.
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
	teams, err := s.d.Gateway.Teams(ctx)
	if err != nil {
		return nil, gatewayError(err)
	}
	from, dates := rangeDays(s.d.Now(), n)
	to := from.AddDate(0, 0, n)
	out := make([]AgentUsage, 0, len(agents))
	for i := range agents {
		a := &agents[i]
		u := AgentUsage{AgentID: a.ID, Days: []DayCount{}}
		if a.Kind == domain.AgentConsole {
			for _, t := range turns.ByAgent {
				if t.AgentID == a.ID.String() || t.AgentID == a.Slug {
					u.Requests += t.Requests
					u.Days = append(u.Days, t.Days...)
				}
			}
			out = append(out, u)
			continue
		}
		j := slices.IndexFunc(teams, func(t bifrost.Team) bool { return t.Name == a.Slug })
		if j < 0 {
			out = append(out, u)
			continue
		}
		perDay := map[string]int{}
		for _, w := range windows(from, to) {
			buckets, reqErr := s.d.Gateway.Requests(ctx, teams[j].ID, w[0], w[1])
			if reqErr != nil {
				return nil, gatewayError(reqErr)
			}
			for _, b := range buckets {
				perDay[b.At.UTC().Format(time.DateOnly)] += b.Count
			}
		}
		for _, d := range dates {
			u.Requests += perDay[d]
			u.Days = append(u.Days, DayCount{Date: d, Requests: perDay[d]})
		}
		out = append(out, u)
	}
	return out, nil
}

// SpendRanges are the ranges the stats take, in days.
var SpendRanges = map[string]int{"7d": 7, "30d": 30, "90d": 90}

// maxSpendDays bounds the days Spend reads.
const maxSpendDays = 366

// spendLagS is how far Bifrost's cost totals may trail real time.
const spendLagS = 60

// Spend reads Bifrost's logged cost of each UTC day from from to to, both
// dates included, in all and by agent slug. A team that is no agent's
// counts in the total only.
func (s *Agent) Spend(ctx context.Context, _ authn.ConsoleMember, from, to string) (Spend, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	start, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return Spend{}, domain.ErrSpendRange.WithField("from", apperr.Invalid, "not a date")
	}
	last, err := time.Parse(time.DateOnly, to)
	if err != nil {
		return Spend{}, domain.ErrSpendRange.WithField("to", apperr.Invalid, "not a date")
	}
	n := int(last.Sub(start).Hours()/24) + 1
	if n < 1 || n > maxSpendDays {
		return Spend{}, domain.ErrSpendRange
	}
	teams, err := s.d.Gateway.Teams(ctx)
	if err != nil {
		return Spend{}, gatewayError(err)
	}
	slugOf := map[string]string{}
	for _, t := range teams {
		slugOf[t.ID] = t.Name
	}
	agents, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return Spend{}, fmt.Errorf("list the agents: %w", err)
	}
	out := Spend{Days: make([]SpendDay, 0, n), LagS: spendLagS}
	at := map[string]int{}
	for i := range n {
		d := start.AddDate(0, 0, i).Format(time.DateOnly)
		at[d] = i
		out.Days = append(out.Days, SpendDay{Date: d, ByAgent: map[string]float64{}})
	}
	for _, w := range windows(start, last.AddDate(0, 0, 1)) {
		buckets, costErr := s.d.Gateway.CostByTeam(ctx, w[0], w[1])
		if costErr != nil {
			return Spend{}, gatewayError(costErr)
		}
		for _, b := range buckets {
			i, ok := at[b.At.UTC().Format(time.DateOnly)]
			if !ok {
				continue
			}
			day := &out.Days[i]
			day.TotalUSD += b.Total
			for team, usd := range b.ByTeam {
				slug := slugOf[team]
				if slug != "" && slices.ContainsFunc(agents, func(a repository.Agent) bool { return a.Slug == slug }) {
					day.ByAgent[slug] += usd
				}
			}
		}
	}
	return out, nil
}

// windowDays is the longest window Bifrost answers in buckets of a day or
// less, which start at UTC midnight.
const windowDays = 31

// windows cuts from to to into windows of windowDays at most.
func windows(from, to time.Time) [][2]time.Time {
	var out [][2]time.Time
	for start := from; start.Before(to); start = start.AddDate(0, 0, windowDays) {
		end := start.AddDate(0, 0, windowDays)
		if end.After(to) {
			end = to
		}
		out = append(out, [2]time.Time{start, end})
	}
	return out
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
