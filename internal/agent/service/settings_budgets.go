package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/litellm"
)

// memberBudget is the LiteLLM budget each member has a day, as an end
// user of the agent service's key: the agent passes user=<member id>.
const memberBudget = "console-member-daily"

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
	daily, err := s.gateway.Budget(ctx, memberBudget)
	switch {
	case err == nil:
		out.MemberDailyUSD = daily.Max
	case !isNotFound(err):
		return Budgets{}, gatewayError(err)
	default:
	}
	return out, nil
}

// UpdateBudgets sets agents' monthly budgets and the members' daily one.
// The daily one is a LiteLLM budget of one day that the agent service's
// key gives each end user it sees.
func (s *Agent) UpdateBudgets(ctx context.Context, caller authn.ConsoleMember, in BudgetsPatch) (Budgets, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	details := map[string]any{}
	for _, p := range in.Agents {
		if v := p.MonthlyBudgetUSD.Value; v != nil && *v < 0 {
			return Budgets{}, domain.ErrAgentInvalid.WithField("monthly_budget_usd", "negative")
		}
		if _, err := s.UpdateAgent(ctx, caller, p.AgentID, AgentPatch{MonthlyBudgetUSD: p.MonthlyBudgetUSD}); err != nil {
			return Budgets{}, err
		}
	}
	if in.MemberDailyUSD.Set {
		v := in.MemberDailyUSD.Value
		if v != nil && *v < 0 {
			return Budgets{}, domain.ErrAgentInvalid.WithField("member_daily_usd", "negative")
		}
		day := "1d"
		if err := s.gateway.SaveBudget(ctx, litellm.Budget{ID: memberBudget, Max: v, Duration: &day}); err != nil {
			return Budgets{}, gatewayError(err)
		}
		keys, err := s.gateway.KeysByAlias(ctx, consoleKey)
		if err != nil {
			return Budgets{}, gatewayError(err)
		}
		for _, k := range keys {
			if err = s.gateway.UpdateKey(ctx, k.Token, map[string]any{"end_user_budget_id": memberBudget}); err != nil {
				return Budgets{}, gatewayError(err)
			}
		}
		details["member_daily_usd"] = v
		if err = s.record(ctx, caller, audit.AIBudgets, audit.AISettings("budgets"), details); err != nil {
			return Budgets{}, err
		}
	}
	return s.Budgets(ctx)
}

// SpendRanges are the ranges Spend takes, in days.
var SpendRanges = map[string]int{"7d": 7, "30d": 30, "90d": 90}

// Spend reads LiteLLM's daily spend over a range ending today, in all and
// by agent; spend on a model name no agent has counts in the total only.
func (s *Agent) Spend(ctx context.Context, rng string) (Spend, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	days, ok := SpendRanges[rng]
	if !ok {
		rng, days = "30d", SpendRanges["30d"]
	}
	end := s.now().UTC()
	start := end.AddDate(0, 0, -(days - 1))
	out := Spend{Range: rng, Start: start.Format(time.DateOnly), End: end.Format(time.DateOnly), Days: []SpendDay{}}
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

func isNotFound(err error) bool { return errors.Is(err, litellm.ErrNotFound) }
