package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/provider/litellm"
)

// The LiteLLM budgets of one day that the console's members have as end
// users of the agent service's key (the agent passes user=<member id>):
// one per role, and one per member with a budget of their own. The
// moderators' budget is also the key's end_user_budget_id, for a member
// LiteLLM has not seen yet.
const (
	moderatorBudget = "console-member-moderator"
	adminBudget     = "console-member-admin"
	memberPrefix    = "console-member-"
)

// The caps' outcomes: stop_agents refuses console turns once the month's
// spend reaches the cap; alert_only only raises the alert.
const (
	OnCapStop  = "stop_agents"
	OnCapAlert = "alert_only"
)

// RoutingStrategies are the router strategies an admin may pick:
// simple-shuffle picks by the providers' weights; within it a lower order
// is tried first and a higher one is the fallback.
var RoutingStrategies = []string{"simple-shuffle", "least-busy", "latency-based-routing", "usage-based-routing-v2"}

// Rotation reads the router's settings.
func (s *Agent) Rotation(ctx context.Context) (Rotation, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	cur, err := s.gateway.RouterSettings(ctx)
	if err != nil {
		return Rotation{}, gatewayError(err)
	}
	r := Rotation{
		RoutingStrategy: infoString(cur, "routing_strategy"), NumRetries: int(infoFloat(cur, "num_retries", 0)),
		AllowedFails: int(infoFloat(cur, "allowed_fails", 0)), CooldownS: infoFloat(cur, "cooldown_time", 0),
		TimeoutS: infoFloat(cur, "timeout", 0),
	}
	if r.RoutingStrategy == "" {
		r.RoutingStrategy = "simple-shuffle"
	}
	return r, nil
}

// UpdateRotation sets the router's settings through LiteLLM's config,
// which every LiteLLM replica applies.
func (s *Agent) UpdateRotation(ctx context.Context, caller authn.ConsoleMember, in RotationPatch) (Rotation, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	set := map[string]any{}
	if in.RoutingStrategy != nil {
		if !slices.Contains(RoutingStrategies, *in.RoutingStrategy) {
			return Rotation{}, apperr.New(apperr.KindInvalid, "ai_rotation_invalid", "no such routing strategy").
				WithField("routing_strategy", "unknown")
		}
		set["routing_strategy"] = *in.RoutingStrategy
	}
	if in.NumRetries != nil {
		set["num_retries"] = *in.NumRetries
	}
	if in.AllowedFails != nil {
		set["allowed_fails"] = *in.AllowedFails
	}
	if in.CooldownS != nil {
		set["cooldown_time"] = *in.CooldownS
	}
	if in.TimeoutS != nil {
		set["timeout"] = *in.TimeoutS
	}
	if len(set) > 0 {
		if err := s.gateway.UpdateRouterSettings(ctx, set); err != nil {
			return Rotation{}, gatewayError(err)
		}
		if err := s.record(ctx, caller, audit.AIRotation, audit.AISettings("rotation"), set); err != nil {
			return Rotation{}, err
		}
	}
	return s.Rotation(ctx)
}

// Budgets reads each agent's monthly budget and what it spent this period
// from its keys, and the members' daily budget.
func (s *Agent) Budgets(ctx context.Context) (Budgets, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	agents, err := s.settings.Agents(ctx)
	if err != nil {
		return Budgets{}, fmt.Errorf("list the agents: %w", err)
	}
	keys := map[string][]litellm.Key{}
	out := Budgets{Agents: make([]AgentBudget, 0, len(agents))}
	for i := range agents {
		a := &agents[i]
		b := AgentBudget{AgentID: a.ID, Name: a.Name, Kind: a.Kind, MonthlyBudgetUSD: a.MonthlyBudgetUSD}
		for _, alias := range a.KeyAliases {
			ks, ok := keys[alias]
			if !ok {
				if ks, err = s.gateway.KeysByAlias(ctx, alias); err != nil {
					return Budgets{}, gatewayError(err)
				}
				keys[alias] = ks
			}
			for _, k := range ks {
				if a.Kind == domain.AgentApp {
					b.SpentUSD += k.Spend
					if b.MonthlyBudgetUSD == nil {
						b.MonthlyBudgetUSD = k.MaxBudget
					}
				} else {
					b.SpentUSD += k.ModelSpend[a.Alias]
				}
			}
		}
		out.Agents = append(out.Agents, b)
	}
	set, err := s.settings.BudgetSettings(ctx)
	if err != nil {
		return Budgets{}, fmt.Errorf("read the budget settings: %w", err)
	}
	spent, err := s.monthSpent(ctx, true)
	if err != nil {
		return Budgets{}, err
	}
	out.MonthlyCapUSD, out.MonthSpentUSD, out.AlertPct, out.OnCap = set.MonthlyCapUSD, spent, set.AlertPct, set.OnCap
	if c := set.MonthlyCapUSD; c != nil {
		out.Alerting = spent >= *c*float64(set.AlertPct)/100
		out.Capped = spent >= *c
	}
	out.MemberDailyUSD = RoleBudgets{Moderator: set.ModeratorDailyUSD, Admin: set.AdminDailyUSD}
	out.Overrides = make([]MemberDailyUSD, 0, len(set.Members))
	for _, m := range set.Members {
		out.Overrides = append(out.Overrides, MemberDailyUSD(m))
	}
	return out, nil
}

// UpdateBudgets sets agents' monthly budgets, the month's cap with its
// alert, and the members' daily budgets. A change to the daily budgets is
// written to LiteLLM at once: the role budgets, a budget per member with
// one of their own, and each member's end user on the budget that applies.
func (s *Agent) UpdateBudgets(ctx context.Context, caller authn.ConsoleMember, in BudgetsPatch) (Budgets, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*gatewayTimeout)
	defer cancel()
	for _, p := range in.Agents {
		if v := p.MonthlyBudgetUSD.Value; v != nil && *v < 0 {
			return Budgets{}, domain.ErrAgentInvalid.WithField("monthly_budget_usd", "negative")
		}
		if _, err := s.UpdateAgent(ctx, caller, p.AgentID, AgentPatch{MonthlyBudgetUSD: p.MonthlyBudgetUSD}); err != nil {
			return Budgets{}, err
		}
	}
	set, err := s.settings.BudgetSettings(ctx)
	if err != nil {
		return Budgets{}, fmt.Errorf("read the budget settings: %w", err)
	}
	details := map[string]any{}
	if in.MonthlyCapUSD.Set {
		set.MonthlyCapUSD, details["monthly_cap_usd"] = in.MonthlyCapUSD.Value, in.MonthlyCapUSD.Value
	}
	if in.AlertPct != nil {
		set.AlertPct, details["alert_pct"] = *in.AlertPct, *in.AlertPct
	}
	if in.OnCap != nil {
		if *in.OnCap != OnCapStop && *in.OnCap != OnCapAlert {
			return Budgets{}, domain.ErrAgentInvalid.WithField("on_cap", "unknown")
		}
		set.OnCap, details["on_cap"] = *in.OnCap, *in.OnCap
	}
	daily := false
	if r := in.MemberDailyUSD; r != nil {
		if r.Moderator.Set {
			set.ModeratorDailyUSD, details["moderator_daily_usd"], daily = noneAtZero(r.Moderator.Value), r.Moderator.Value, true
		}
		if r.Admin.Set {
			set.AdminDailyUSD, details["admin_daily_usd"], daily = noneAtZero(r.Admin.Value), r.Admin.Value, true
		}
	}
	if in.Overrides != nil {
		set.SetMembers, set.Members, daily = true, make([]repository.MemberBudget, 0, len(*in.Overrides)), true
		for _, o := range *in.Overrides {
			set.Members = append(set.Members, repository.MemberBudget(o))
		}
		details["overrides"] = len(set.Members)
	}
	if len(details) == 0 && len(in.Agents) > 0 {
		return s.Budgets(ctx)
	}
	for _, v := range []*float64{set.MonthlyCapUSD, set.ModeratorDailyUSD, set.AdminDailyUSD} {
		if v != nil && *v < 0 {
			return Budgets{}, domain.ErrAgentInvalid.WithField("budget", "negative")
		}
	}
	if daily {
		if err = s.applyMemberBudgets(ctx, set); err != nil {
			return Budgets{}, err
		}
	}
	by := caller.MemberID
	set.UpdatedBy, set.UpdatedAt = &by, s.now()
	err = s.settings.SaveBudgetSettings(ctx, set, audit.Entry{
		Actor: audit.ByMember(caller), Action: audit.AIBudgets, Target: audit.AISettings("budgets"), Details: details,
		At: set.UpdatedAt,
	})
	if errors.Is(err, repository.ErrNotFound) {
		return Budgets{}, domain.ErrMemberNotFound
	}
	if err != nil {
		return Budgets{}, fmt.Errorf("save the budget settings: %w", err)
	}
	s.forgetMemberBudgets()
	return s.Budgets(ctx)
}

// applyMemberBudgets writes set's daily budgets to LiteLLM: the two role
// budgets, one per member with a budget of their own, the moderators'
// budget as the agent service key's default, and each member's end user on
// the budget that applies to them.
func (s *Agent) applyMemberBudgets(ctx context.Context, set repository.BudgetSettings) error {
	day := "1d"
	budgets := map[string]*float64{moderatorBudget: set.ModeratorDailyUSD, adminBudget: set.AdminDailyUSD}
	for _, m := range set.Members {
		budgets[memberPrefix+m.MemberID.String()] = new(m.DailyUSD)
	}
	for id, most := range budgets {
		if err := s.gateway.SaveBudget(ctx, litellm.Budget{ID: id, Max: most, Duration: &day}); err != nil {
			return gatewayError(err)
		}
	}
	keys, err := s.gateway.KeysByAlias(ctx, consoleKey)
	if err != nil {
		return gatewayError(err)
	}
	for _, k := range keys {
		if err = s.gateway.UpdateKey(ctx, k.Token, map[string]any{"end_user_budget_id": moderatorBudget}); err != nil {
			return gatewayError(err)
		}
	}
	members, err := s.members(ctx)
	if err != nil {
		return err
	}
	for _, m := range members {
		if err = s.gateway.SetEndUserBudget(ctx, m.ID.String(), budgetOf(set, m.ID, string(m.Role))); err != nil {
			return gatewayError(err)
		}
	}
	return nil
}

// noneAtZero is v, or nil for 0: a role's daily budget of 0 is no budget.
func noneAtZero(v *float64) *float64 {
	if v != nil && *v == 0 {
		return nil
	}
	return v
}

// budgetOf is the LiteLLM budget of a member with role.
func budgetOf(set repository.BudgetSettings, member uuid.UUID, role string) string {
	if slices.ContainsFunc(set.Members, func(m repository.MemberBudget) bool { return m.MemberID == member }) {
		return memberPrefix + member.String()
	}
	if role == "admin" {
		return adminBudget
	}
	return moderatorBudget
}

// members lists every console member, every page.
func (s *Agent) members(ctx context.Context) ([]consolesvc.MemberView, error) {
	var out []consolesvc.MemberView
	req := page.Request{Limit: 100}
	for {
		p, err := s.console.ListMembers(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list the console members: %w", err)
		}
		out = append(out, p.Items...)
		if p.NextCursor == nil {
			return out, nil
		}
		req.Cursor = *p.NextCursor
	}
}

// monthCacheFor is how long the month's spend is reused for the cap
// check of a turn.
const monthCacheFor = time.Minute

// monthSpent is LiteLLM's spend since the first of this month (UTC), kept
// a minute for the turns' cap check; fresh reads it again.
func (s *Agent) monthSpent(ctx context.Context, fresh bool) (float64, error) {
	now := s.now()
	s.mu.Lock()
	if !fresh && now.Sub(s.monthAt) < monthCacheFor {
		v := s.month
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	utc := now.UTC()
	first := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	days, err := s.gateway.DailySpend(ctx, first.Format(time.DateOnly), utc.Format(time.DateOnly))
	if err != nil {
		return 0, gatewayError(err)
	}
	var total float64
	for _, d := range days {
		total += d.Spend
	}
	s.mu.Lock()
	s.month, s.monthAt = total, now
	s.mu.Unlock()
	return total, nil
}

// capReached says whether console turns stop: the cap is set with
// stop_agents and the month's spend has reached it. A LiteLLM that cannot
// be read stops nothing; LiteLLM's own budgets still hold.
func (s *Agent) capReached(ctx context.Context, set repository.BudgetSettings) bool {
	if set.OnCap != OnCapStop || set.MonthlyCapUSD == nil {
		return false
	}
	spent, err := s.monthSpent(ctx, false)
	return err == nil && spent >= *set.MonthlyCapUSD
}

// ensureMemberBudget puts the member's end user on the budget that applies
// to them, once per process and budget, so a member made after the last
// budget change has theirs before the first turn. A failure is left to the
// next turn. With no daily budget set there is nothing to put them on.
func (s *Agent) ensureMemberBudget(ctx context.Context, set repository.BudgetSettings, caller authn.ConsoleMember) {
	if set.ModeratorDailyUSD == nil && set.AdminDailyUSD == nil && len(set.Members) == 0 {
		return
	}
	want := budgetOf(set, caller.MemberID, caller.Role)
	s.mu.Lock()
	done := s.budgetOf[caller.MemberID] == want
	s.mu.Unlock()
	if done {
		return
	}
	if err := s.gateway.SetEndUserBudget(ctx, caller.MemberID.String(), want); err != nil {
		return
	}
	s.mu.Lock()
	s.budgetOf[caller.MemberID] = want
	s.mu.Unlock()
}

// forgetMemberBudgets drops what ensureMemberBudget remembers, after the
// budgets changed.
func (s *Agent) forgetMemberBudgets() {
	s.mu.Lock()
	clear(s.budgetOf)
	s.mu.Unlock()
}

// SpendRanges are the ranges Spend takes, in days.
var SpendRanges = map[string]int{"7d": 7, "30d": 30, "90d": 90}

// Spend reads LiteLLM's daily spend over a range ending today, in all and
// by agent; spend on a model name no agent has counts in the total only.
func (s *Agent) Spend(ctx context.Context, caller authn.ConsoleMember, rng string) (Spend, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	days, ok := SpendRanges[rng]
	if !ok {
		rng, days = "30d", SpendRanges["30d"]
	}
	end := s.now().UTC()
	start := end.AddDate(0, 0, -(days - 1))
	out := Spend{Range: rng, Start: start.Format(time.DateOnly), End: end.Format(time.DateOnly), Days: []SpendDay{}}
	members, err := s.memberSpend(ctx, caller, rng, out.Start, out.End)
	if err != nil {
		return Spend{}, err
	}
	out.Members = members
	got, err := s.gateway.DailySpend(ctx, out.Start, out.End)
	if err != nil {
		return Spend{}, gatewayError(err)
	}
	agents, err := s.settings.Agents(ctx)
	if err != nil {
		return Spend{}, fmt.Errorf("list the agents: %w", err)
	}
	for _, d := range got {
		day := SpendDay{Date: d.Date, TotalUSD: d.Spend, Agents: []AgentSpend{}}
		for i := range agents {
			a := &agents[i]
			if usd, ok := d.ByModel[a.Alias]; ok {
				day.Agents = append(day.Agents, AgentSpend{AgentID: a.ID, USD: usd})
			}
		}
		out.TotalUSD += d.Spend
		out.Days = append(out.Days, day)
	}
	slices.SortFunc(out.Days, func(a, b SpendDay) int {
		switch {
		case a.Date < b.Date:
			return -1
		case a.Date > b.Date:
			return 1
		default:
			return 0
		}
	})
	return out, nil
}
