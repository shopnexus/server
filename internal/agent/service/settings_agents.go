package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/litellm"
)

// consoleKey is the LiteLLM key of the agent service, whose allowed models
// gain each console agent's alias.
const consoleKey = "agent"

// budgetPeriod is the period of an agent's monthly budget in LiteLLM.
const budgetPeriod = "30d"

func agentView(a repository.Agent) AgentView {
	v := AgentView{
		ID: a.ID, Kind: a.Kind, Name: a.Name, Alias: a.Alias, Icon: a.Icon, Purpose: a.Purpose,
		RequiredTags: a.RequiredTags, Model: a.Model, Instructions: a.Instructions,
		InstructionsVersion: a.InstructionsVersion, Tools: a.Tools, Limits: a.Limits,
		MonthlyBudgetUSD: a.MonthlyBudgetUSD, Enabled: a.Enabled, UpdatedBy: a.UpdatedBy, UpdatedAt: a.UpdatedAt,
	}
	if v.RequiredTags == nil {
		v.RequiredTags = []string{}
	}
	if v.Tools == nil {
		v.Tools = []string{}
	}
	return v
}

// Agents lists every agent, app agents first.
func (s *Agent) Agents(ctx context.Context) ([]AgentView, error) {
	rows, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the agents: %w", err)
	}
	out := make([]AgentView, 0, len(rows))
	for i := range rows {
		a := &rows[i]
		out = append(out, agentView(*a))
	}
	return out, nil
}

// ConsoleAgents lists the console agents a member may pick: those turned
// on, the Console assistant marked as the default.
func (s *Agent) ConsoleAgents(ctx context.Context) ([]ConsoleAgent, error) {
	rows, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the agents: %w", err)
	}
	out := []ConsoleAgent{}
	for i := range rows {
		a := &rows[i]
		if a.Kind == domain.AgentConsole && a.Enabled {
			out = append(out, ConsoleAgent{
				ID: a.ID, Name: a.Name, Icon: a.Icon, Purpose: a.Purpose, Default: a.ID == domain.ConsoleAssistant,
			})
		}
	}
	return out, nil
}

// AgentVersions lists an agent's instructions, newest first.
func (s *Agent) AgentVersions(ctx context.Context, id uuid.UUID) ([]AgentVersion, error) {
	if _, err := s.agentOf(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.d.Settings.Versions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list the agent's versions: %w", err)
	}
	out := make([]AgentVersion, 0, len(rows))
	for _, v := range rows {
		out = append(out, AgentVersion{Version: v.Version, Instructions: v.Instructions, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt})
	}
	return out, nil
}

// checkModel refuses a model that lacks a tag the agent requires, or a tag
// that is not in ai_tags.
func (s *Agent) checkModel(ctx context.Context, required []string, model string) error {
	tags, err := s.d.Settings.Tags(ctx)
	if err != nil {
		return fmt.Errorf("read the tags: %w", err)
	}
	for _, t := range required {
		if !slices.ContainsFunc(tags, func(k repository.Tag) bool { return k.Name == t }) {
			return domain.ErrTagUnknown.WithField("required_tags", apperr.Unknown, t)
		}
	}
	modelTags, err := s.d.Settings.ModelTags(ctx)
	if err != nil {
		return fmt.Errorf("read the model tags: %w", err)
	}
	if missing := domain.MissingTags(required, modelTags[model]); len(missing) > 0 {
		return domain.ErrModelLacksTags.WithField("model", apperr.NotAllowed, strings.Join(missing, ", "))
	}
	return nil
}

// CreateAgent makes a console agent: the row and its first version, then
// LiteLLM from the row: its alias's deployments for its model, the alias
// in the agent service's key and its budget. The same create again finds
// the row and writes LiteLLM again, so a create LiteLLM failed is retried.
func (s *Agent) CreateAgent(ctx context.Context, caller authn.ConsoleMember, in NewAgent) (AgentView, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	in.Name = strings.TrimSpace(in.Name)
	limits, err := in.Limits.Check(domain.AgentConsole)
	if err != nil {
		return AgentView{}, err
	}
	if in.Name == "" || !domain.ValidAlias(in.Alias) || (in.MonthlyBudgetUSD != nil && *in.MonthlyBudgetUSD < 0) {
		return AgentView{}, domain.ErrAgentInvalid
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	by := caller.MemberID
	a := repository.Agent{
		ID: uuid.NewV7(), Kind: domain.AgentConsole, Name: in.Name, Alias: in.Alias, Icon: in.Icon, Purpose: in.Purpose,
		RequiredTags: nonNil(in.RequiredTags), Model: in.Model, Instructions: in.Instructions, InstructionsVersion: 1,
		Tools: nonNil(in.Tools), Limits: limits, MonthlyBudgetUSD: in.MonthlyBudgetUSD, KeyAliases: []string{consoleKey},
		Enabled: enabled, UpdatedBy: &by, UpdatedAt: s.d.Now(), WriteID: uuid.NewV7(),
	}
	all, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return AgentView{}, fmt.Errorf("list the agents: %w", err)
	}
	if i := slices.IndexFunc(all, func(o repository.Agent) bool { return o.Name == a.Name || o.Alias == a.Alias }); i >= 0 {
		if !sameAgent(all[i], a) {
			return AgentView{}, domain.ErrAgentTaken
		}
		// A retry of this create: the row is there, LiteLLM may lack it.
		saved, applyErr := s.applyAgent(ctx, all[i], nil, false, true)
		if applyErr != nil {
			return AgentView{}, applyErr
		}
		return agentView(saved), nil
	}
	if err = s.checkModel(ctx, a.RequiredTags, a.Model); err != nil {
		return AgentView{}, err
	}
	st, err := s.readState(ctx)
	if err != nil {
		return AgentView{}, err
	}
	if len(st.providersOf(a.Model)) == 0 {
		return AgentView{}, domain.ErrModelNotFound
	}
	// An alias in LiteLLM that no agent has is another's, such as one of
	// LiteLLM's config.
	if slices.ContainsFunc(st.deps, func(d litellm.Deployment) bool { return d.ModelName == a.Alias }) {
		return AgentView{}, domain.ErrAgentTaken
	}
	err = s.d.Settings.CreateAgent(ctx, a, audit.Entry{
		Actor: audit.ByMember(caller), Action: audit.AIAgentCreate, Target: audit.AIAgent(a.ID),
		Details: map[string]any{"name": a.Name, "alias": a.Alias, "model": a.Model}, At: a.UpdatedAt,
	})
	if errors.Is(err, repository.ErrTaken) {
		return AgentView{}, domain.ErrAgentTaken
	}
	if err != nil {
		return AgentView{}, fmt.Errorf("create the agent: %w", err)
	}
	saved, err := s.applyAgent(ctx, a, &st, false, true)
	if err != nil {
		return AgentView{}, err
	}
	return agentView(saved), nil
}

// sameAgent says whether a stored agent is what a create makes, so the
// create is a retry.
func sameAgent(stored, made repository.Agent) bool {
	return stored.Kind == made.Kind && stored.Name == made.Name && stored.Alias == made.Alias &&
		stored.Icon == made.Icon && stored.Purpose == made.Purpose && slices.Equal(stored.RequiredTags, made.RequiredTags) &&
		stored.Model == made.Model && stored.Instructions == made.Instructions && slices.Equal(stored.Tools, made.Tools) &&
		stored.Limits == made.Limits && sameBudget(stored.MonthlyBudgetUSD, made.MonthlyBudgetUSD) &&
		stored.Enabled == made.Enabled
}

func sameBudget(a, b *float64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// UpdateAgent changes an agent. An app agent takes a model, limits and a
// budget only. The row is saved first; a change of model or budget then
// goes to LiteLLM from the saved row, and new instructions are a new
// version. The same change again writes LiteLLM again.
func (s *Agent) UpdateAgent(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, in AgentPatch) (AgentView, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	a, err := s.agentOf(ctx, id)
	if err != nil {
		return AgentView{}, err
	}
	if a.Kind == domain.AgentApp && (in.Name != nil || in.Icon != nil || in.Purpose != nil || in.RequiredTags != nil ||
		in.Instructions != nil || in.Tools != nil || in.Enabled != nil) {
		return AgentView{}, domain.ErrAppAgentFixed
	}
	read := a.WriteID
	details := map[string]any{}
	if in.Name != nil {
		if a.Name = strings.TrimSpace(*in.Name); a.Name == "" {
			return AgentView{}, domain.ErrAgentInvalid
		}
		details["name"] = a.Name
	}
	if in.Icon != nil {
		a.Icon, details["icon"] = *in.Icon, *in.Icon
	}
	if in.Purpose != nil {
		a.Purpose, details["purpose"] = *in.Purpose, true
	}
	if in.RequiredTags != nil {
		a.RequiredTags, details["required_tags"] = nonNil(*in.RequiredTags), *in.RequiredTags
	}
	if in.Tools != nil {
		a.Tools, details["tools"] = nonNil(*in.Tools), *in.Tools
	}
	if in.Limits != nil {
		if a.Limits, err = in.Limits.Check(a.Kind); err != nil {
			return AgentView{}, err
		}
		details["limits"] = a.Limits
	}
	if in.Enabled != nil {
		a.Enabled, details["enabled"] = *in.Enabled, *in.Enabled
	}
	newVersion := in.Instructions != nil && *in.Instructions != a.Instructions
	if newVersion {
		a.Instructions = *in.Instructions
		a.InstructionsVersion++
		details["instructions_version"] = a.InstructionsVersion
	}
	newModel := in.Model != nil && *in.Model != a.Model
	if in.Model != nil {
		a.Model = *in.Model
	}
	if newModel || in.RequiredTags != nil {
		if err = s.checkModel(ctx, a.RequiredTags, a.Model); err != nil {
			return AgentView{}, err
		}
	}
	var st *state
	if in.Model != nil {
		cur, stateErr := s.readState(ctx)
		if stateErr != nil {
			return AgentView{}, stateErr
		}
		if newModel && len(cur.providersOf(a.Model)) == 0 {
			return AgentView{}, domain.ErrModelNotFound
		}
		st = &cur
		if newModel {
			details["model"] = a.Model
		}
	}
	if in.MonthlyBudgetUSD.Set {
		if v := in.MonthlyBudgetUSD.Value; v != nil && *v < 0 {
			return AgentView{}, domain.ErrAgentInvalid.WithField("monthly_budget_usd", apperr.OutOfRange, "negative")
		}
		a.MonthlyBudgetUSD, details["monthly_budget_usd"] = in.MonthlyBudgetUSD.Value, in.MonthlyBudgetUSD.Value
	}
	by := caller.MemberID
	a.UpdatedBy, a.UpdatedAt, a.WriteID = &by, s.d.Now(), uuid.NewV7()
	err = s.d.Settings.SaveAgent(ctx, a, read, newVersion, audit.Entry{
		Actor: audit.ByMember(caller), Action: audit.AIAgentUpdate, Target: audit.AIAgent(a.ID), Details: details, At: a.UpdatedAt,
	})
	switch {
	case errors.Is(err, repository.ErrConflict):
		return AgentView{}, apperr.ErrTryAgain
	case errors.Is(err, repository.ErrTaken):
		return AgentView{}, domain.ErrAgentTaken
	case err != nil:
		return AgentView{}, fmt.Errorf("save the agent: %w", err)
	}
	if in.Model == nil && !in.MonthlyBudgetUSD.Set {
		return agentView(a), nil
	}
	if a, err = s.applyAgent(ctx, a, st, newModel, in.MonthlyBudgetUSD.Set); err != nil {
		return AgentView{}, err
	}
	return agentView(a), nil
}

// applyRounds bounds how often applyAgent follows saves that landed while
// it wrote LiteLLM.
const applyRounds = 3

// applyAgent writes LiteLLM from a's row: the alias's deployments for a
// new model or when they serve another, a console agent's alias in the
// agent service's key, and with budget its monthly budget. When another
// save landed meanwhile it applies that row too, so LiteLLM ends on the
// newest; st is LiteLLM's state when the caller has just read it.
func (s *Agent) applyAgent(ctx context.Context, a repository.Agent, st *state, newModel, budget bool) (repository.Agent, error) {
	for range applyRounds {
		if st == nil {
			read, err := s.readState(ctx)
			if err != nil {
				return a, err
			}
			st = &read
		}
		if newModel || !st.aliasServes(a.Alias, a.Model) {
			if err := s.writeAlias(ctx, *st, a.Alias, a.Model); err != nil {
				return a, err
			}
		}
		if a.Kind == domain.AgentConsole {
			if err := s.allowAlias(ctx, a.Alias); err != nil {
				return a, err
			}
		}
		if budget {
			if err := s.applyBudget(ctx, a, a.MonthlyBudgetUSD); err != nil {
				return a, err
			}
		}
		cur, err := s.agentOf(ctx, a.ID)
		if err != nil {
			return a, err
		}
		if cur.WriteID == a.WriteID {
			return cur, nil
		}
		a, st, newModel, budget = cur, nil, false, true
	}
	return a, nil
}

// allowAlias adds alias to the agent service's key, when that key names
// its models; a key with no list may call every model already.
func (s *Agent) allowAlias(ctx context.Context, alias string) error {
	keys, err := s.d.Gateway.KeysByAlias(ctx, consoleKey)
	if err != nil {
		return gatewayError(err)
	}
	for _, k := range keys {
		if len(k.Models) == 0 || slices.Contains(k.Models, alias) {
			continue
		}
		if err = s.d.Gateway.UpdateKey(ctx, k.Token, map[string]any{"models": append(k.Models, alias)}); err != nil {
			return gatewayError(err)
		}
	}
	return nil
}

// applyBudget writes an agent's monthly budget to LiteLLM, nil for none:
// an app agent's is the budget of each of its keys; a console agent's is
// its alias's budget on the agent service's key.
func (s *Agent) applyBudget(ctx context.Context, a repository.Agent, budget *float64) error {
	for _, alias := range a.KeyAliases {
		keys, err := s.d.Gateway.KeysByAlias(ctx, alias)
		if err != nil {
			return gatewayError(err)
		}
		for _, k := range keys {
			fields := map[string]any{}
			if a.Kind == domain.AgentApp {
				fields["max_budget"] = budget
				if budget != nil {
					fields["budget_duration"] = budgetPeriod
				}
			} else {
				per := map[string]any{}
				for model, b := range k.ModelMaxBudget {
					per[model] = b
				}
				delete(per, a.Alias)
				if budget != nil {
					per[a.Alias] = map[string]any{"max_budget": *budget, "budget_duration": budgetPeriod}
				}
				fields["model_max_budget"] = per
			}
			if err = s.d.Gateway.UpdateKey(ctx, k.Token, fields); err != nil {
				return gatewayError(err)
			}
		}
	}
	return nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
