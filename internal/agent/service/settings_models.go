package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
)

// Models lists every model an agent uses, as its model or a fallback, or
// that has a price or tags, with its catalog's price, and the tags an
// admin may set.
func (s *Agent) Models(ctx context.Context) (ModelList, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	tags, err := s.d.Settings.Tags(ctx)
	if err != nil {
		return ModelList{}, fmt.Errorf("read the tags: %w", err)
	}
	modelTags, err := s.d.Settings.ModelTags(ctx)
	if err != nil {
		return ModelList{}, fmt.Errorf("read the model tags: %w", err)
	}
	prices, err := s.d.Settings.Prices(ctx)
	if err != nil {
		return ModelList{}, fmt.Errorf("read the prices: %w", err)
	}
	agents, err := s.d.Settings.Agents(ctx)
	if err != nil {
		return ModelList{}, fmt.Errorf("read the agents: %w", err)
	}
	byModel := map[domain.ModelRef]*Model{}
	model := func(ref domain.ModelRef) *Model {
		m, ok := byModel[ref]
		if !ok {
			m = &Model{Provider: ref.Provider, Model: ref.Model, Tags: nonNil(modelTags[ref]), Agents: []AgentRef{}}
			byModel[ref] = m
		}
		return m
	}
	for ref := range modelTags {
		model(ref)
	}
	for _, p := range prices {
		model(p.Model).Price = &Price{Input: p.Input, Output: p.Output, CacheRead: p.CacheRead, CacheWrite: p.CacheWrite}
	}
	for i := range agents {
		a := &agents[i]
		for _, ref := range append([]domain.ModelRef{a.Model}, a.Fallbacks...) {
			m := model(ref)
			if !slices.ContainsFunc(m.Agents, func(r AgentRef) bool { return r.ID == a.ID }) {
				m.Agents = append(m.Agents, AgentRef{ID: a.ID, Name: a.Name})
			}
		}
	}
	catalogs := map[string]map[string]*Price{}
	for ref, m := range byModel {
		catalog, ok := catalogs[ref.Provider]
		if !ok {
			list, _, listErr := s.d.Gateway.Models(ctx, ref.Provider, catalogLimit)
			if listErr != nil {
				return ModelList{}, gatewayError(listErr)
			}
			catalog = map[string]*Price{}
			for _, c := range list {
				catalog[c.Name] = catalogPrice(c)
			}
			catalogs[ref.Provider] = catalog
		}
		m.CatalogPrice = catalog[ref.Model]
	}
	out := ModelList{Items: make([]Model, 0, len(byModel)), Tags: make([]Tag, 0, len(tags))}
	for _, m := range byModel {
		out.Items = append(out.Items, *m)
	}
	slices.SortFunc(out.Items, func(a, b Model) int {
		return cmp.Or(cmp.Compare(a.Provider, b.Provider), cmp.Compare(a.Model, b.Model))
	})
	for _, t := range tags {
		out.Tags = append(out.Tags, Tag{Name: t.Name, Kind: t.Kind})
	}
	return out, nil
}

// SetModelTags replaces a model's tags. An agent that requires a tag the
// model loses keeps running on it; the agents page shows the gap.
func (s *Agent) SetModelTags(ctx context.Context, caller authn.ConsoleMember, in ModelTags) (ModelTags, error) {
	ref := domain.ModelRef{Provider: in.Provider, Model: in.Model}
	tags := slices.Clone(in.Tags)
	slices.Sort(tags)
	tags = slices.Compact(tags)
	err := s.d.Settings.SetModelTags(ctx, ref, nonNil(tags), audit.Entry{
		Actor: audit.ByMember(caller), Action: audit.AIModelTags, Target: audit.AIModel(ref.String()),
		Details: map[string]any{"model": ref.String(), "tags": tags}, At: s.d.Now(),
	})
	if errors.Is(err, repository.ErrUnknownTag) {
		return ModelTags{}, domain.ErrTagUnknown
	}
	if err != nil {
		return ModelTags{}, fmt.Errorf("set model tags: %w", err)
	}
	all, err := s.d.Settings.ModelTags(ctx)
	if err != nil {
		return ModelTags{}, fmt.Errorf("read the model tags: %w", err)
	}
	return ModelTags{Provider: ref.Provider, Model: ref.Model, Tags: nonNil(all[ref])}, nil
}

// SetModelPrice writes a model's price, a new one or over the old, then
// reconciles Bifrost, whose logged cost of each call follows it. When
// Bifrost cannot take it the price stays saved and the next reconcile
// writes it.
func (s *Agent) SetModelPrice(ctx context.Context, caller authn.ConsoleMember, in ModelPrice) (ModelPrice, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	if in.Input < 0 || in.Output < 0 || (in.CacheRead != nil && *in.CacheRead < 0) || (in.CacheWrite != nil && *in.CacheWrite < 0) {
		return ModelPrice{}, domain.ErrPriceInvalid
	}
	if err := s.checkProviders(ctx, in.Provider); err != nil {
		return ModelPrice{}, err
	}
	by := caller.MemberID
	ref := domain.ModelRef{Provider: in.Provider, Model: in.Model}
	p := repository.Price{
		Model: ref, Input: in.Input, Output: in.Output, CacheRead: in.CacheRead, CacheWrite: in.CacheWrite,
		UpdatedBy: &by, UpdatedAt: s.d.Now(),
	}
	if err := s.d.Settings.SetPrice(ctx, p, audit.Entry{
		Actor: audit.ByMember(caller), Action: audit.AIModelPrice, Target: audit.AIModel(ref.String()),
		Details: map[string]any{"model": ref.String(), "price": in.Price}, At: p.UpdatedAt,
	}); err != nil {
		return ModelPrice{}, fmt.Errorf("set the model's price: %w", err)
	}
	if err := s.Reconcile(ctx); err != nil {
		return ModelPrice{}, err
	}
	return in, nil
}
