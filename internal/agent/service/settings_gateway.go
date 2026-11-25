package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/litellm"
)

// gatewayTimeout bounds one settings call, which may make several calls to
// LiteLLM's admin API.
const gatewayTimeout = 30 * time.Second

// The deployments the server writes in LiteLLM carry model_info.shopnexus,
// their role, and model_info.provider and model_info.model:
//
//   - catalog: a model a provider serves, model_name "<provider>/<model>";
//     the provider's model list.
//   - alias: an agent's alias, one per provider that has the agent's model,
//     with that provider's weight and order.
//   - lister: "<provider>/*" with the provider's route, which LiteLLM's
//     /model/info expands into the models the provider lists; a provider
//     in sync mode has one.
const (
	roleKey     = "shopnexus"
	roleCatalog = "catalog"
	roleAlias   = "alias"
	roleLister  = "lister"
)

// The credential_info keys of a provider. custom_llm_provider is LiteLLM's
// own, which its UI shows.
const (
	infoKind     = "custom_llm_provider"
	infoBaseURL  = "base_url"
	infoWeight   = "weight"
	infoOrder    = "order"
	infoMode     = "mode"
	infoSyncedAt = "synced_at"
	infoEnabled  = "enabled"
)

// gatewayError is what a member sees of a LiteLLM failure: its refusal as
// ai_gateway_refused with the detail logged, anything else as
// ai_gateway_unavailable.
func gatewayError(err error) error {
	if errors.Is(err, litellm.ErrRefused) {
		return domain.ErrGatewayRefused.WithCause(err)
	}
	return domain.ErrGatewayUnavailable.WithCause(err)
}

// state is LiteLLM's credentials and deployments, read once per change.
type state struct {
	creds []litellm.Credential
	deps  []litellm.Deployment
}

func (s *Agent) readState(ctx context.Context) (state, error) {
	creds, err := s.d.Gateway.Credentials(ctx)
	if err != nil {
		return state{}, gatewayError(err)
	}
	deps, err := s.d.Gateway.Deployments(ctx)
	if err != nil {
		return state{}, gatewayError(err)
	}
	return state{creds: creds, deps: deps}, nil
}

func (st state) credential(id string) (litellm.Credential, bool) {
	i := slices.IndexFunc(st.creds, func(c litellm.Credential) bool { return c.Name == id })
	if i < 0 {
		return litellm.Credential{}, false
	}
	return st.creds[i], true
}

// role is a deployment's role, or "" for one the server did not write.
func role(d litellm.Deployment) string { return d.InfoString(roleKey) }

// catalog lists provider's models, each once, sorted.
func (st state) catalog(provider string) []string {
	var out []string
	for _, d := range st.deps {
		if role(d) == roleCatalog && d.InfoString("provider") == provider {
			out = append(out, d.InfoString("model"))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// providersOf lists the enabled providers that serve model, in the
// credentials' order.
func (st state) providersOf(model string) []litellm.Credential {
	var out []litellm.Credential
	for _, c := range st.creds {
		if enabled(c) && slices.Contains(st.catalog(c.Name), model) {
			out = append(out, c)
		}
	}
	return out
}

// enabled says a provider serves the aliases: info's enabled, true when
// unset.
func enabled(c litellm.Credential) bool {
	v, ok := c.Info[infoEnabled].(bool)
	return !ok || v
}

// providerOf is the Provider view of c.
func (st state) providerOf(c litellm.Credential) Provider {
	p := Provider{
		ID: c.Name, Name: c.DisplayName, Kind: infoString(c.Info, infoKind), BaseURL: infoString(c.Info, infoBaseURL),
		KeyHint: c.Values["api_key"], Weight: infoFloat(c.Info, infoWeight, 1), Order: int(infoFloat(c.Info, infoOrder, 1)),
		Mode: domain.ProviderMode(infoString(c.Info, infoMode)), Enabled: enabled(c), Models: st.catalog(c.Name),
	}
	if p.Name == "" {
		p.Name = c.Name
	}
	if p.Mode == "" {
		p.Mode = domain.ModePick
	}
	if p.Models == nil {
		p.Models = []string{}
	}
	if t, err := time.Parse(time.RFC3339, infoString(c.Info, infoSyncedAt)); err == nil {
		p.SyncedAt = &t
	}
	return p
}

func infoString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func infoFloat(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return def
}

// route is the litellm_params.model of a provider's model: its kind, the
// LiteLLM provider route, then the model.
func route(kind, model string) string { return kind + "/" + model }

// modelOfRoute is the model of a LiteLLM route, without the route's first
// segment: openai/baai/bge-m3 is baai/bge-m3.
func modelOfRoute(r string) string {
	_, model, ok := strings.Cut(r, "/")
	if !ok {
		return r
	}
	return model
}

// catalogDeployment is the deployment that puts model in provider's list.
func catalogDeployment(p Provider, model string, cost litellm.Params) litellm.Deployment {
	return litellm.Deployment{
		ModelName: p.ID + "/" + model,
		Params: litellm.Params{
			Model: route(p.Kind, model), Credential: p.ID, InputCost: cost.InputCost, OutputCost: cost.OutputCost,
		},
		Info: map[string]any{roleKey: roleCatalog, "provider": p.ID, "model": model},
	}
}

// writeAlias rewrites alias's deployments: one per provider that serves
// model, with that provider's weight and order and the catalog's price.
// It adds the new ones before it deletes the old, so the alias always has
// a deployment; a model no provider serves is ErrModelNotFound and
// changes nothing.
func (s *Agent) writeAlias(ctx context.Context, st state, alias, model string) error {
	serving := st.providersOf(model)
	if len(serving) == 0 {
		return domain.ErrModelNotFound
	}
	var old []string
	for _, d := range st.deps {
		if d.ModelName == alias && d.ID() != "" {
			old = append(old, d.ID())
		}
	}
	for _, c := range serving {
		p := st.providerOf(c)
		w, o := p.Weight, p.Order
		cost := st.cost(p.ID, model)
		if err := s.d.Gateway.AddDeployment(ctx, litellm.Deployment{
			ModelName: alias,
			Params: litellm.Params{
				Model: route(p.Kind, model), Credential: p.ID, Weight: &w, Order: &o,
				InputCost: cost.InputCost, OutputCost: cost.OutputCost,
			},
			Info: map[string]any{roleKey: roleAlias, "provider": p.ID, "model": model},
		}); err != nil {
			return gatewayError(err)
		}
	}
	for _, id := range old {
		if err := s.d.Gateway.DeleteDeployment(ctx, id); err != nil {
			return gatewayError(err)
		}
	}
	return nil
}

// aliasServes says whether alias has a deployment and each one the server
// wrote serves model. One of LiteLLM's config is left to a new model, since
// its route need not name the agent's model.
func (st state) aliasServes(alias, model string) bool {
	n := 0
	for _, d := range st.deps {
		if d.ModelName != alias {
			continue
		}
		if role(d) == roleAlias && d.InfoString("model") != model {
			return false
		}
		n++
	}
	return n > 0
}

// cost is the price on provider's catalog deployment of model.
func (st state) cost(provider, model string) litellm.Params {
	for _, d := range st.deps {
		if role(d) == roleCatalog && d.InfoString("provider") == provider && d.InfoString("model") == model {
			return d.Params
		}
	}
	return litellm.Params{}
}

// rewriteAliases rewrites the alias of every agent on a model provider
// serves, after the provider's weight, order or models changed. An agent
// whose model no provider serves any more keeps its deployments.
func (s *Agent) rewriteAliases(ctx context.Context, provider string) error {
	agents, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return fmt.Errorf("read the agents: %w", err)
	}
	st, err := s.readState(ctx)
	if err != nil {
		return err
	}
	for i := range agents {
		a := &agents[i]
		uses := slices.ContainsFunc(st.deps, func(d litellm.Deployment) bool {
			return d.ModelName == a.Alias && d.InfoString("provider") == provider
		})
		if !uses && !slices.ContainsFunc(st.providersOf(a.Model), func(c litellm.Credential) bool { return c.Name == provider }) {
			continue
		}
		if err = s.writeAlias(ctx, st, a.Alias, a.Model); err != nil && !errors.Is(err, domain.ErrModelNotFound) {
			return err
		}
	}
	return nil
}

// inUse refuses to take model away from provider when an agent's model
// would be left with no provider.
func (s *Agent) inUse(ctx context.Context, st state, provider string, models []string) error {
	agents, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return fmt.Errorf("read the agents: %w", err)
	}
	for i := range agents {
		a := &agents[i]
		if !slices.Contains(models, a.Model) {
			continue
		}
		others := slices.DeleteFunc(st.providersOf(a.Model), func(c litellm.Credential) bool { return c.Name == provider })
		if len(others) == 0 {
			return domain.ErrProviderInUse.WithField("agent", apperr.InUse, a.Name)
		}
	}
	return nil
}

// record writes the console action of a change made in LiteLLM.
func (s *Agent) record(ctx context.Context, caller authn.ConsoleMember, action audit.ActionKind, target audit.Target,
	details map[string]any,
) error {
	if err := s.d.Settings.Record(ctx, audit.Entry{
		Actor: audit.ByMember(caller), Action: action, Target: target, Details: details, At: s.d.Now(),
	}); err != nil {
		return fmt.Errorf("record %s: %w", action, err)
	}
	return nil
}

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
