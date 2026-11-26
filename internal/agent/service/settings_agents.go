package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/bifrost"
)

// agentViews are the views of agents, each with its budget's spend this
// month from its team in Bifrost; a spend Bifrost cannot give stays null.
func (s *Agent) agentViews(ctx context.Context, agents []repository.Agent) []AgentView {
	teams, err := s.d.Gateway.Teams(ctx)
	if err != nil {
		s.d.Log.WarnContext(ctx, "agent: read the teams' spend", "err", err)
	}
	now := s.d.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]AgentView, 0, len(agents))
	for i := range agents {
		a := &agents[i]
		v := AgentView{
			ID: a.ID, Kind: a.Kind, Name: a.Name, Slug: a.Slug, Icon: a.Icon, Purpose: a.Purpose,
			RequiredTags: nonNil(a.RequiredTags), Model: a.Model, Fallbacks: nonNil(a.Fallbacks), Instructions: a.Instructions,
			InstructionsVersion: a.InstructionsVersion, Tools: nonNil(a.Tools), Limits: a.Limits,
			MonthlyBudgetUSD: a.MonthlyBudgetUSD, Callers: make([]Caller, 0, len(a.Callers)), Enabled: a.Enabled,
			UpdatedBy: a.UpdatedBy, UpdatedAt: a.UpdatedAt,
		}
		for _, c := range a.Callers {
			v.Callers = append(v.Callers, Caller(c))
		}
		j := slices.IndexFunc(teams, func(t bifrost.Team) bool { return t.Name == a.Slug })
		if a.MonthlyBudgetUSD != nil && j >= 0 && len(teams[j].Budgets) == 1 {
			// A window Bifrost has not reset yet, for want of a call this
			// month, spent nothing this month.
			spent, b := 0.0, teams[j].Budgets[0]
			if !b.LastReset.Before(month) {
				spent = b.CurrentUsage
			}
			v.BudgetSpentUSD, v.BudgetResetsAt = &spent, new(month.AddDate(0, 1, 0))
		}
		out = append(out, v)
	}
	return out
}

// Agents lists every agent, app agents first.
func (s *Agent) Agents(ctx context.Context) ([]AgentView, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	rows, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the agents: %w", err)
	}
	return s.agentViews(ctx, rows), nil
}

// agentView is one agent's view.
func (s *Agent) agentView(ctx context.Context, a repository.Agent) AgentView {
	return s.agentViews(ctx, []repository.Agent{a})[0]
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

// checkModels refuses an agent whose models do not fit: a required tag
// ai_tags lacks, a fallback that repeats a model, a fallback of an
// embedding agent (another model's vectors are in another space), a model
// or fallback that lacks a required tag or has no price. A price is only
// set on a provider Bifrost runs, so this reads the database alone and a
// change saves while Bifrost is down.
func (s *Agent) checkModels(ctx context.Context, a repository.Agent) error {
	tags, err := s.d.Settings.Tags(ctx)
	if err != nil {
		return fmt.Errorf("read the tags: %w", err)
	}
	for _, t := range a.RequiredTags {
		if !slices.ContainsFunc(tags, func(k repository.Tag) bool { return k.Name == t }) {
			return domain.ErrTagUnknown.WithField("required_tags", apperr.Unknown, t)
		}
	}
	if len(a.Fallbacks) > 0 && slices.Contains(a.RequiredTags, domain.TagEmbedding) {
		return domain.ErrAgentInvalid.WithField("fallbacks", apperr.NotAllowed, "an embedding agent takes no fallback")
	}
	models := append([]domain.ModelRef{a.Model}, a.Fallbacks...)
	for i, m := range models {
		if slices.Contains(models[:i], m) {
			return domain.ErrAgentInvalid.WithField("fallbacks", apperr.Duplicate, m.String())
		}
	}
	modelTags, err := s.d.Settings.ModelTags(ctx)
	if err != nil {
		return fmt.Errorf("read the model tags: %w", err)
	}
	prices, err := s.d.Settings.Prices(ctx)
	if err != nil {
		return fmt.Errorf("read the prices: %w", err)
	}
	for i, m := range models {
		field := "model"
		if i > 0 {
			field = "fallbacks"
		}
		if missing := domain.MissingTags(a.RequiredTags, modelTags[m]); len(missing) > 0 {
			return domain.ErrModelLacksTags.WithField(field, apperr.NotAllowed, m.String()+": "+strings.Join(missing, ", "))
		}
		if !slices.ContainsFunc(prices, func(p repository.Price) bool { return p.Model == m }) {
			return domain.ErrModelUnpriced.WithField(field, apperr.NotAllowed, m.String())
		}
	}
	return nil
}

// CreateAgent makes a console agent with one caller, its slug: the row,
// its first version and its caller, then Bifrost from the rows. The same
// create again finds the row and reconciles again, so a create Bifrost
// failed is retried; the periodic reconcile also writes it. It is
// UC-MOD-12 A1.
func (s *Agent) CreateAgent(ctx context.Context, caller authn.ConsoleMember, in NewAgent) (AgentView, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	in.Name = strings.TrimSpace(in.Name)
	limits, err := in.Limits.Check(domain.AgentConsole)
	if err != nil {
		return AgentView{}, err
	}
	if in.Name == "" || !domain.ValidAgentSlug(in.Slug) {
		return AgentView{}, domain.ErrAgentInvalid
	}
	if in.MonthlyBudgetUSD != nil && *in.MonthlyBudgetUSD <= 0 {
		return AgentView{}, domain.ErrAgentInvalid.WithField("monthly_budget_usd", apperr.OutOfRange, "not above 0")
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	by := caller.MemberID
	a := repository.Agent{
		ID: uuid.NewV7(), Kind: domain.AgentConsole, Name: in.Name, Slug: in.Slug, Icon: in.Icon, Purpose: in.Purpose,
		RequiredTags: nonNil(in.RequiredTags), Model: in.Model, Fallbacks: nonNil(in.Fallbacks), ExtraBody: map[string]any{},
		Instructions: in.Instructions, InstructionsVersion: 1, Tools: nonNil(in.Tools), Limits: limits,
		MonthlyBudgetUSD: in.MonthlyBudgetUSD, Callers: []repository.Caller{{Name: in.Slug}}, Enabled: enabled,
		UpdatedBy: &by, UpdatedAt: s.d.Now(), WriteID: uuid.NewV7(),
	}
	all, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return AgentView{}, fmt.Errorf("list the agents: %w", err)
	}
	if i := slices.IndexFunc(all, func(o repository.Agent) bool { return o.Name == a.Name || o.Slug == a.Slug }); i >= 0 {
		// E5
		if !sameAgent(all[i], a) {
			return AgentView{}, domain.ErrAgentTaken
		}
		// A retry of this create: the row is there, Bifrost may lack it.
		if err = s.Reconcile(ctx); err != nil {
			return AgentView{}, err
		}
		return s.agentView(ctx, all[i]), nil
	}
	if err = s.checkModels(ctx, a); err != nil {
		return AgentView{}, err
	}
	err = s.d.Settings.CreateAgent(ctx, a, audit.Entry{
		Actor: audit.ByMember(caller), Action: audit.AIAgentCreate, Target: audit.AIAgent(a.ID),
		Details: map[string]any{"name": a.Name, "slug": a.Slug, "model": a.Model.String()}, At: a.UpdatedAt,
	})
	if errors.Is(err, repository.ErrTaken) {
		return AgentView{}, domain.ErrAgentTaken
	}
	if err != nil {
		return AgentView{}, fmt.Errorf("create the agent: %w", err)
	}
	if err = s.Reconcile(ctx); err != nil {
		return AgentView{}, err
	}
	return s.agentView(ctx, a), nil
}

// sameAgent says whether a stored agent is what a create makes, so the
// create is a retry.
func sameAgent(stored, made repository.Agent) bool {
	return stored.Kind == made.Kind && stored.Name == made.Name && stored.Slug == made.Slug &&
		stored.Icon == made.Icon && stored.Purpose == made.Purpose && slices.Equal(stored.RequiredTags, made.RequiredTags) &&
		stored.Model == made.Model && slices.Equal(stored.Fallbacks, made.Fallbacks) &&
		stored.Instructions == made.Instructions && slices.Equal(stored.Tools, made.Tools) &&
		stored.Limits == made.Limits && samePtr(stored.MonthlyBudgetUSD, made.MonthlyBudgetUSD) &&
		stored.Enabled == made.Enabled
}

// UpdateAgent changes an agent. An app agent takes a model, fallbacks,
// limits, a budget and its callers' rate limits only. The row is saved
// first; a change Bifrost carries (model, fallbacks, budget, callers, on
// or off) then reconciles it, and new instructions are a new version. A
// new model drops the request fields of the old one (extra_body). It is
// UC-MOD-12 steps 2 to 4.
func (s *Agent) UpdateAgent(ctx context.Context, caller authn.ConsoleMember, id uuid.UUID, in AgentPatch) (AgentView, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	a, err := s.agentOf(ctx, id)
	if err != nil {
		return AgentView{}, err
	}
	// E3
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
	// A3
	newVersion := in.Instructions != nil && *in.Instructions != a.Instructions
	if newVersion {
		a.Instructions = *in.Instructions
		a.InstructionsVersion++
		details["instructions_version"] = a.InstructionsVersion
	}
	if in.Model != nil && *in.Model != a.Model {
		a.Model, a.ExtraBody, details["model"] = *in.Model, map[string]any{}, in.Model.String()
	}
	if in.Fallbacks != nil {
		a.Fallbacks = nonNil(*in.Fallbacks)
		names := make([]string, 0, len(a.Fallbacks))
		for _, f := range a.Fallbacks {
			names = append(names, f.String())
		}
		details["fallbacks"] = names
	}
	// E1, E2, E4
	if in.Model != nil || in.Fallbacks != nil || in.RequiredTags != nil {
		if err = s.checkModels(ctx, a); err != nil {
			return AgentView{}, err
		}
	}
	if in.MonthlyBudgetUSD.Set {
		if v := in.MonthlyBudgetUSD.Value; v != nil && *v <= 0 {
			return AgentView{}, domain.ErrAgentInvalid.WithField("monthly_budget_usd", apperr.OutOfRange, "not above 0")
		}
		a.MonthlyBudgetUSD, details["monthly_budget_usd"] = in.MonthlyBudgetUSD.Value, in.MonthlyBudgetUSD.Value
	}
	if in.Callers != nil {
		// The names are the server's code's, which reads its keys by them:
		// the list names each caller once, and only its limits change.
		byName := map[string]repository.Caller{}
		for _, c := range *in.Callers {
			if _, twice := byName[c.Name]; twice {
				return AgentView{}, domain.ErrAgentInvalid.WithField("callers", apperr.Duplicate, c.Name)
			}
			byName[c.Name] = repository.Caller(c)
		}
		have := make([]string, 0, len(a.Callers))
		for _, c := range a.Callers {
			have = append(have, c.Name)
		}
		if !sameSet(slices.Collect(maps.Keys(byName)), have) {
			return AgentView{}, domain.ErrAgentInvalid.WithField("callers", apperr.NotAllowed,
				"name each of the agent's callers: "+strings.Join(have, ", "))
		}
		for i := range a.Callers {
			a.Callers[i] = byName[a.Callers[i].Name]
		}
		details["callers"] = *in.Callers
	}
	// 3. The row and its audit entry, under the write it read.
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
	// 4. Bifrost from the rows; A2 when it cannot be reached.
	if in.Model != nil || in.Fallbacks != nil || in.MonthlyBudgetUSD.Set || in.Callers != nil || in.Enabled != nil {
		if err = s.Reconcile(ctx); err != nil {
			return AgentView{}, err
		}
	}
	return s.agentView(ctx, a), nil
}
