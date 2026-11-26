package service

import (
	"context"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/provider/bifrost"
)

// catalogLimit is more models than any provider's catalog lists.
const catalogLimit = 5000

// A provider is down when at least half of the last hour's requests
// failed, from 5 requests on, and degraded from 5% failed with 20 requests.
const (
	downShare     = 0.5
	downMin       = 5
	degradedShare = 0.05
	degradedMin   = 20
)

// Providers lists the providers Bifrost runs from its config.json, each
// with its status over the last hour and the models of its catalog.
func (s *Agent) Providers(ctx context.Context) ([]Provider, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	list, err := s.d.Gateway.Providers(ctx)
	if err != nil {
		return nil, gatewayError(err)
	}
	now := s.d.Now()
	out := make([]Provider, 0, len(list))
	for _, p := range list {
		if p.Status == "deleted" {
			continue
		}
		v := Provider{Name: p.Name, Kind: p.Name}
		if p.Custom != "" {
			v.Kind = "custom"
		}
		if u, parseErr := url.Parse(p.BaseURL); parseErr == nil {
			v.BaseHost = u.Host
		}
		stats, statsErr := s.d.Gateway.ProviderStats(ctx, p.Name, now.Add(-time.Hour), now)
		if statsErr != nil {
			return nil, gatewayError(statsErr)
		}
		if _, v.ModelCount, err = s.d.Gateway.Models(ctx, p.Name, 1); err != nil {
			return nil, gatewayError(err)
		}
		v.Requests1h = stats.Requests
		if stats.Requests > 0 {
			v.ErrorShare1h = math.Round(10000-stats.SuccessRate*100) / 10000
		}
		switch n := stats.Requests; {
		case p.Status == "error", v.ErrorShare1h >= downShare && n >= downMin:
			v.Status = StatusDown
		case n == 0:
			v.Status = StatusUnknown
		case v.ErrorShare1h >= degradedShare && n >= degradedMin:
			v.Status = StatusDegraded
		default:
			v.Status = StatusOK
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b Provider) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// ProviderModels lists a provider's catalog in Bifrost with list prices,
// which a new model's price starts from.
func (s *Agent) ProviderModels(ctx context.Context, name string) ([]CatalogModel, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	if err := s.checkProviders(ctx, name); err != nil {
		return nil, err
	}
	models, _, err := s.d.Gateway.Models(ctx, name, catalogLimit)
	if err != nil {
		return nil, gatewayError(err)
	}
	out := make([]CatalogModel, 0, len(models))
	for _, m := range models {
		out = append(out, CatalogModel{Model: m.Name, CatalogPrice: catalogPrice(m), MaxInputTokens: m.MaxInput})
	}
	slices.SortFunc(out, func(a, b CatalogModel) int { return strings.Compare(a.Model, b.Model) })
	return out, nil
}

// checkProviders refuses a provider Bifrost does not run.
func (s *Agent) checkProviders(ctx context.Context, names ...string) error {
	list, err := s.d.Gateway.Providers(ctx)
	if err != nil {
		return gatewayError(err)
	}
	for _, n := range names {
		if !slices.ContainsFunc(list, func(p bifrost.Provider) bool { return p.Name == n && p.Status != "deleted" }) {
			return domain.ErrProviderNotFound
		}
	}
	return nil
}

// catalogPrice is a catalog model's list price, nil when the catalog has
// no input price.
func catalogPrice(m bifrost.Model) *Price {
	if m.Input == nil {
		return nil
	}
	p := &Price{Input: *m.Input, CacheRead: m.CacheRead, CacheWrite: m.CacheWrite}
	if m.Output != nil {
		p.Output = *m.Output
	}
	return p
}
