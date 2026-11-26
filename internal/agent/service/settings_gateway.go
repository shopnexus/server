package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/provider/bifrost"
)

// gatewayTimeout bounds one settings call, which may make several calls to
// Bifrost's admin API.
const gatewayTimeout = 30 * time.Second

// ReconcileEvery is how often the reconciler runs besides after each
// settings write, so a change made in Bifrost's UI or a write Bifrost
// missed is put right.
const ReconcileEvery = 5 * time.Minute

// overridePrefix starts the name of each pricing override the server
// writes; the rest is provider/model.
const overridePrefix = "server:"

// rateWindow is the window of every caller's rate limits.
const rateWindow = "1m"

// gatewayError is what a member sees of a Bifrost failure: its refusal as
// ai_gateway_refused with the detail logged, anything else as
// ai_gateway_unavailable.
func gatewayError(err error) error {
	if errors.Is(err, bifrost.ErrRefused) {
		return domain.ErrGatewayRefused.WithCause(err)
	}
	return domain.ErrGatewayUnavailable.WithCause(err)
}

// Reconcile writes the AI policy into Bifrost: a team per agent with its
// monthly budget, a virtual key per caller with its agent's models and its
// rate limits, and a pricing override per priced model. It deletes the
// keys and overrides the server wrote that nothing names any more, turns
// off the keys of agents turned off, and hands the ring the keys. One
// process at a time runs it.
func (s *Agent) Reconcile(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	err := s.d.Settings.Exclusive(ctx, func(ctx context.Context) error {
		agents, err := s.d.Settings.Agents(ctx)
		if err != nil {
			return fmt.Errorf("read the agents: %w", err)
		}
		prices, err := s.d.Settings.Prices(ctx)
		if err != nil {
			return fmt.Errorf("read the prices: %w", err)
		}
		teams, err := s.d.Gateway.Teams(ctx)
		if err != nil {
			return gatewayError(err)
		}
		keys, err := s.d.Gateway.VirtualKeys(ctx)
		if err != nil {
			return gatewayError(err)
		}
		overrides, err := s.d.Gateway.PricingOverrides(ctx)
		if err != nil {
			return gatewayError(err)
		}
		// Teams are named after the agents' slugs. An agent is never
		// deleted, so no team is.
		callers := map[string]bool{}
		for i := range agents {
			a := &agents[i]
			j := slices.IndexFunc(teams, func(t bifrost.Team) bool { return t.Name == a.Slug })
			var team bifrost.Team
			switch {
			case j < 0:
				if team, err = s.d.Gateway.CreateTeam(ctx, a.Slug, a.MonthlyBudgetUSD); err != nil {
					return gatewayError(err)
				}
			case !teamHas(teams[j], a.MonthlyBudgetUSD):
				team = teams[j]
				if err = s.d.Gateway.UpdateTeam(ctx, team.ID, a.Slug, a.MonthlyBudgetUSD); err != nil {
					return gatewayError(err)
				}
			default:
				team = teams[j]
			}
			for _, c := range a.Callers {
				callers[c.Name] = true
				want := keyOf(*a, c, team.ID)
				k := slices.IndexFunc(keys, func(v bifrost.VirtualKey) bool { return v.Name == c.Name })
				switch {
				case k < 0:
					_, err = s.d.Gateway.CreateVirtualKey(ctx, want)
				case !keyHas(keys[k], want):
					err = s.d.Gateway.UpdateVirtualKey(ctx, keys[k].ID, want)
				default: // as Bifrost has it
				}
				if err != nil {
					return gatewayError(err)
				}
			}
		}
		for _, k := range keys {
			if k.Metadata[bifrost.MetaOwner] == bifrost.OwnerServer && !callers[k.Name] {
				if err = s.d.Gateway.DeleteVirtualKey(ctx, k.ID); err != nil {
					return gatewayError(err)
				}
			}
		}
		priced := map[string]bool{}
		for _, p := range prices {
			name := overridePrefix + p.Model.String()
			priced[name] = true
			price := bifrost.Price{Input: p.Input, Output: p.Output, CacheRead: p.CacheRead, CacheWrite: p.CacheWrite}
			o := slices.IndexFunc(overrides, func(v bifrost.PricingOverride) bool { return v.Name == name })
			switch {
			case o < 0:
				err = s.d.Gateway.CreatePricingOverride(ctx, name, p.Model.Provider, p.Model.Model, price)
			case overrides[o].Provider != p.Model.Provider || overrides[o].Pattern != p.Model.Model ||
				!maps.Equal(overrides[o].Patch, bifrost.PricePatch(price)):
				err = s.d.Gateway.UpdatePricingOverride(ctx, overrides[o].ID, name, p.Model.Provider, p.Model.Model, price)
			default: // as Bifrost has it
			}
			if err != nil {
				return gatewayError(err)
			}
		}
		for _, o := range overrides {
			if strings.HasPrefix(o.Name, overridePrefix) && !priced[o.Name] {
				if err = s.d.Gateway.DeletePricingOverride(ctx, o.ID); err != nil {
					return gatewayError(err)
				}
			}
		}
		if keys, err = s.d.Gateway.VirtualKeys(ctx); err != nil {
			return gatewayError(err)
		}
		s.d.Ring.Set(keys)
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconcile Bifrost: %w", err)
	}
	return nil
}

// teamHas says whether t carries monthly as its one calendar-aligned
// monthly budget, or none for nil.
func teamHas(t bifrost.Team, monthly *float64) bool {
	if monthly == nil {
		return len(t.Budgets) == 0
	}
	return len(t.Budgets) == 1 && t.Budgets[0].MaxLimit == *monthly && t.Budgets[0].ResetDuration == "1M" && t.CalendarAligned
}

// keyOf is the virtual key caller c of agent a should be: under a's team,
// on a when a is on, allowed a's model and fallbacks exactly, with c's
// rate limits and metadata that tell the ring what c calls.
func keyOf(a repository.Agent, c repository.Caller, teamID string) bifrost.KeyInput {
	models := map[string][]string{}
	fallbacks := make([]string, 0, len(a.Fallbacks))
	for _, m := range append([]domain.ModelRef{a.Model}, a.Fallbacks...) {
		if !slices.Contains(models[m.Provider], m.Model) {
			models[m.Provider] = append(models[m.Provider], m.Model)
		}
	}
	for _, f := range a.Fallbacks {
		fallbacks = append(fallbacks, f.String())
	}
	meta := map[string]string{
		bifrost.MetaOwner: bifrost.OwnerServer, bifrost.MetaAgent: a.Slug, bifrost.MetaModel: a.Model.String(),
	}
	if len(fallbacks) > 0 {
		meta[bifrost.MetaFallbacks] = bifrost.JoinList(fallbacks)
	}
	if len(a.ExtraBody) > 0 {
		// The map encodes as a JSON object, which cannot fail.
		raw, _ := json.Marshal(a.ExtraBody)
		meta[bifrost.MetaExtraBody] = string(raw)
	}
	in := bifrost.KeyInput{Name: c.Name, TeamID: teamID, Models: models, Active: a.Enabled, Metadata: meta}
	window := rateWindow
	if c.TokensPerMin != nil {
		in.Rate.TokenMaxLimit, in.Rate.TokenResetDuration = new(int64(*c.TokensPerMin)), &window
	}
	if c.RequestsPerMin != nil {
		in.Rate.RequestMaxLimit, in.Rate.RequestResetDuration = new(int64(*c.RequestsPerMin)), &window
	}
	return in
}

// keyHas says whether k is what in writes.
func keyHas(k bifrost.VirtualKey, in bifrost.KeyInput) bool {
	if k.TeamID == nil || *k.TeamID != in.TeamID || k.Active() != in.Active || !maps.Equal(k.Metadata, in.Metadata) ||
		len(k.ProviderConfigs) != len(in.Models) {
		return false
	}
	for _, pc := range k.ProviderConfigs {
		want, ok := in.Models[pc.Provider]
		if !ok || !pc.AllowAllKeys || !sameSet(pc.AllowedModels, want) {
			return false
		}
	}
	var have bifrost.RateLimit
	if k.RateLimit != nil {
		have = *k.RateLimit
	}
	return samePtr(have.TokenMaxLimit, in.Rate.TokenMaxLimit) && samePtr(have.RequestMaxLimit, in.Rate.RequestMaxLimit) &&
		(have.TokenMaxLimit == nil || samePtr(have.TokenResetDuration, in.Rate.TokenResetDuration)) &&
		(have.RequestMaxLimit == nil || samePtr(have.RequestResetDuration, in.Rate.RequestResetDuration))
}

func sameSet(a, b []string) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(s string) bool { return !slices.Contains(b, s) })
}

func samePtr[T comparable](a, b *T) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// agentOf maps the repository's not found.
func (s *Agent) agentOf(ctx context.Context, id uuid.UUID) (repository.Agent, error) {
	a, err := s.d.Settings.Agent(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.Agent{}, domain.ErrAgentNotFound
	}
	if err != nil {
		return repository.Agent{}, fmt.Errorf("read the agent: %w", err)
	}
	return a, nil
}

func nonNil[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}
