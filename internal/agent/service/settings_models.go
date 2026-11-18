package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Models lists every model a provider serves or an agent runs on, with its
// tags, its providers, its price per million tokens and its agents, and
// the tags an admin may set.
func (s *Agent) Models(ctx context.Context) (ModelList, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return ModelList{}, err
	}
	tags, err := s.settings.Tags(ctx)
	if err != nil {
		return ModelList{}, fmt.Errorf("read the tags: %w", err)
	}
	modelTags, err := s.settings.ModelTags(ctx)
	if err != nil {
		return ModelList{}, fmt.Errorf("read the model tags: %w", err)
	}
	agents, err := s.settings.Agents(ctx)
	if err != nil {
		return ModelList{}, fmt.Errorf("read the agents: %w", err)
	}
	byModel := map[string]*Model{}
	model := func(id string) *Model {
		m, ok := byModel[id]
		if !ok {
			m = &Model{Model: id, Tags: modelTags[id], Providers: []Named{}, Agents: []AgentRef{}}
			if m.Tags == nil {
				m.Tags = []string{}
			}
			byModel[id] = m
		}
		return m
	}
	names := map[string]string{}
	for _, c := range st.creds {
		names[c.Name] = st.providerOf(c).Name
	}
	for _, d := range st.deps {
		var id, provider string
		switch {
		case role(d) == roleCatalog:
			id, provider = d.InfoString("model"), d.InfoString("provider")
		case role(d) == "" && slices.ContainsFunc(agents, func(a repository.Agent) bool { return a.Alias == d.ModelName }):
			// An alias LiteLLM's config wrote before the server managed it.
			id, provider = modelOfRoute(d.Params.Model), d.Params.Credential
		default:
			continue
		}
		m := model(id)
		if provider != "" && !slices.ContainsFunc(m.Providers, func(n Named) bool { return n.ID == provider }) {
			m.Providers = append(m.Providers, Named{ID: provider, Name: names[provider]})
		}
		if m.InputCostPerMTok == nil && d.Params.InputCost != nil {
			m.InputCostPerMTok, m.OutputCostPerMTok = perMTok(d.Params.InputCost), perMTok(d.Params.OutputCost)
		}
	}
	for i := range agents {
		a := &agents[i]
		m := model(a.Model)
		m.Agents = append(m.Agents, AgentRef{ID: a.ID, Name: a.Name})
	}
	out := ModelList{Items: make([]Model, 0, len(byModel)), Tags: make([]Tag, 0, len(tags))}
	for _, m := range byModel {
		out.Items = append(out.Items, *m)
	}
	slices.SortFunc(out.Items, func(a, b Model) int {
		switch {
		case a.Model < b.Model:
			return -1
		case a.Model > b.Model:
			return 1
		default:
			return 0
		}
	})
	for _, t := range tags {
		out.Tags = append(out.Tags, Tag{Name: t.Name, Kind: t.Kind})
	}
	return out, nil
}

// SetModelTags replaces a model's tags. An agent that requires a tag the
// model loses keeps running on it; the agents page shows the gap.
func (s *Agent) SetModelTags(ctx context.Context, caller authn.ConsoleMember, model string, tags []string) ([]string, error) {
	if model == "" {
		return nil, domain.ErrModelNotFound
	}
	slices.Sort(tags)
	tags = slices.Compact(tags)
	err := s.settings.SetModelTags(ctx, model, tags, audit.Entry{
		Actor: audit.ByMember(caller), Action: audit.AIModelTags, Target: audit.AIModel(model),
		Details: map[string]any{"model": model, "tags": tags}, At: s.now(),
	})
	if errors.Is(err, repository.ErrUnknownTag) {
		return nil, domain.ErrTagUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("set model tags: %w", err)
	}
	all, err := s.settings.ModelTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the model tags: %w", err)
	}
	if all[model] == nil {
		return []string{}, nil
	}
	return all[model], nil
}

func perMTok(perToken *float64) *float64 {
	if perToken == nil {
		return nil
	}
	v := *perToken * 1e6
	return &v
}
