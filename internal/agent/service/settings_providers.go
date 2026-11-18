package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/litellm"
)

// Providers lists LiteLLM's credentials as providers, with their models.
func (s *Agent) Providers(ctx context.Context) ([]Provider, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Provider, 0, len(st.creds))
	for _, c := range st.creds {
		out = append(out, st.providerOf(c))
	}
	return out, nil
}

// CreateProvider stores a provider as a LiteLLM credential. A provider in
// sync mode gets its lister and its models at once.
func (s *Agent) CreateProvider(ctx context.Context, caller authn.ConsoleMember, in NewProvider) (Provider, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	if in.Mode == "" {
		in.Mode = domain.ModePick
	}
	w, o := 1.0, 1
	if in.Weight != nil {
		w = *in.Weight
	}
	if in.Order != nil {
		o = *in.Order
	}
	if !domain.ValidProviderID(in.ID) || !domain.ValidProviderKind(in.Kind) || in.APIKey == "" || w < 0 || o < 1 {
		return Provider{}, domain.ErrProviderInvalid
	}
	st, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	if _, ok := st.credential(in.ID); ok {
		return Provider{}, domain.ErrProviderExists
	}
	values := map[string]string{"api_key": in.APIKey}
	if in.BaseURL != "" {
		values["api_base"] = in.BaseURL
	}
	name := in.Name
	if name == "" {
		name = in.ID
	}
	cred := litellm.Credential{
		Name: in.ID, DisplayName: name, Values: values,
		Info: map[string]any{infoKind: in.Kind, infoBaseURL: in.BaseURL, infoWeight: w, infoOrder: o, infoMode: string(in.Mode)},
	}
	if err = s.gateway.CreateCredential(ctx, cred); err != nil {
		return Provider{}, gatewayError(err)
	}
	p := st.providerOf(cred)
	p.KeyHint = ""
	// Every provider has a lister, which lists its models for the admin;
	// sync mode also adds them.
	if _, err = s.ensureLister(ctx, st, p); err != nil {
		return Provider{}, err
	}
	if in.Mode == domain.ModeSync {
		if p, err = s.syncProvider(ctx, p); err != nil {
			return Provider{}, err
		}
	}
	if err = s.record(ctx, caller, audit.AIProviderCreate, audit.AIProvider(in.ID), map[string]any{
		"provider": in.ID, "kind": in.Kind, "mode": string(in.Mode), "weight": w, "order": o,
	}); err != nil {
		return Provider{}, err
	}
	return p, nil
}

// UpdateProvider changes a provider's name, base URL, key, weight, order
// or mode. A new weight or order is written onto its alias deployments; a
// change to sync mode lists its models at once.
func (s *Agent) UpdateProvider(ctx context.Context, caller authn.ConsoleMember, id string, in ProviderPatch) (Provider, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	c, ok := st.credential(id)
	if !ok {
		return Provider{}, domain.ErrProviderNotFound
	}
	if (in.Weight != nil && *in.Weight < 0) || (in.Order != nil && *in.Order < 1) ||
		(in.Mode != nil && *in.Mode != domain.ModePick && *in.Mode != domain.ModeSync) || (in.APIKey != nil && *in.APIKey == "") {
		return Provider{}, domain.ErrProviderInvalid
	}
	patch := litellm.Credential{Name: id, DisplayName: c.DisplayName, Info: map[string]any{}}
	details := map[string]any{"provider": id}
	if in.Name != nil {
		patch.DisplayName, details["name"] = *in.Name, *in.Name
	}
	values := map[string]string{}
	if in.BaseURL != nil {
		values["api_base"], patch.Info[infoBaseURL], details["base_url"] = *in.BaseURL, *in.BaseURL, *in.BaseURL
	}
	if in.APIKey != nil {
		values["api_key"], details["key_changed"] = *in.APIKey, true
	}
	if len(values) > 0 {
		patch.Values = values
	}
	if in.Weight != nil {
		patch.Info[infoWeight], details["weight"] = *in.Weight, *in.Weight
	}
	if in.Order != nil {
		patch.Info[infoOrder], details["order"] = *in.Order, *in.Order
	}
	if in.Mode != nil {
		patch.Info[infoMode], details["mode"] = string(*in.Mode), string(*in.Mode)
	}
	if in.Enabled != nil {
		if !*in.Enabled {
			// Turning a provider off takes it out of every alias, so an
			// agent whose model only it serves stops the change.
			if err = s.inUse(ctx, st, id, st.catalog(id)); err != nil {
				return Provider{}, err
			}
		}
		patch.Info[infoEnabled], details["enabled"] = *in.Enabled, *in.Enabled
	}
	if err = s.gateway.UpdateCredential(ctx, patch); err != nil {
		return Provider{}, gatewayError(err)
	}
	if in.Weight != nil || in.Order != nil || in.Enabled != nil {
		if err = s.rewriteAliases(ctx, id); err != nil {
			return Provider{}, err
		}
	}
	if st, err = s.readState(ctx); err != nil {
		return Provider{}, err
	}
	c, _ = st.credential(id)
	p := st.providerOf(c)
	if in.Mode != nil && *in.Mode == domain.ModeSync {
		if p, err = s.syncProvider(ctx, p); err != nil {
			return Provider{}, err
		}
	}
	if err = s.record(ctx, caller, audit.AIProviderUpdate, audit.AIProvider(id), details); err != nil {
		return Provider{}, err
	}
	return p, nil
}

// DeleteProvider deletes a provider and every deployment on it. A provider
// that is an agent's model's only one is refused.
func (s *Agent) DeleteProvider(ctx context.Context, caller authn.ConsoleMember, id string) error {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return err
	}
	if _, ok := st.credential(id); !ok {
		return domain.ErrProviderNotFound
	}
	if err = s.inUse(ctx, st, id, st.catalog(id)); err != nil {
		return err
	}
	for _, d := range st.deps {
		if d.InfoString("provider") == id && d.ID() != "" && role(d) != "" {
			if err = s.gateway.DeleteDeployment(ctx, d.ID()); err != nil {
				return gatewayError(err)
			}
		}
	}
	if err = s.gateway.DeleteCredential(ctx, id); err != nil && !errors.Is(err, litellm.ErrNotFound) {
		return gatewayError(err)
	}
	return s.record(ctx, caller, audit.AIProviderDelete, audit.AIProvider(id), map[string]any{"provider": id})
}

// AddProviderModel puts model in a provider's list, with its price per
// million tokens when the admin gives one.
func (s *Agent) AddProviderModel(ctx context.Context, caller authn.ConsoleMember, id, model string, in, out *float64) (Provider, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	c, ok := st.credential(id)
	if !ok {
		return Provider{}, domain.ErrProviderNotFound
	}
	p := st.providerOf(c)
	if !slices.Contains(p.Models, model) {
		if err = s.gateway.AddDeployment(ctx, catalogDeployment(p, model, litellm.Params{
			InputCost: perToken(in), OutputCost: perToken(out),
		})); err != nil {
			return Provider{}, gatewayError(err)
		}
		p.Models = append(p.Models, model)
		slices.Sort(p.Models)
	}
	if err = s.rewriteAliases(ctx, id); err != nil {
		return Provider{}, err
	}
	if err = s.record(ctx, caller, audit.AIProviderModels, audit.AIProvider(id), map[string]any{
		"provider": id, "added": model,
	}); err != nil {
		return Provider{}, err
	}
	return p, nil
}

// RemoveProviderModel takes model out of a provider's list and off the
// aliases it served. The only provider of an agent's model is refused.
func (s *Agent) RemoveProviderModel(ctx context.Context, caller authn.ConsoleMember, id, model string) (Provider, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	c, ok := st.credential(id)
	if !ok {
		return Provider{}, domain.ErrProviderNotFound
	}
	if !slices.Contains(st.catalog(id), model) {
		return Provider{}, domain.ErrModelNotFound
	}
	if err = s.inUse(ctx, st, id, []string{model}); err != nil {
		return Provider{}, err
	}
	for _, d := range st.deps {
		r := role(d)
		if (r == roleCatalog || r == roleAlias) && d.InfoString("provider") == id && d.InfoString("model") == model {
			if err = s.gateway.DeleteDeployment(ctx, d.ID()); err != nil {
				return Provider{}, gatewayError(err)
			}
		}
	}
	p := st.providerOf(c)
	p.Models = slices.DeleteFunc(p.Models, func(m string) bool { return m == model })
	if err = s.record(ctx, caller, audit.AIProviderModels, audit.AIProvider(id), map[string]any{
		"provider": id, "removed": model,
	}); err != nil {
		return Provider{}, err
	}
	return p, nil
}

// TestProvider makes one call through LiteLLM with the provider's key, to
// model or the first model in its list.
func (s *Agent) TestProvider(ctx context.Context, id, model string) (ProviderTest, error) {
	ctx, cancel := context.WithTimeout(ctx, gatewayTimeout)
	defer cancel()
	st, err := s.readState(ctx)
	if err != nil {
		return ProviderTest{}, err
	}
	c, ok := st.credential(id)
	if !ok {
		return ProviderTest{}, domain.ErrProviderNotFound
	}
	p := st.providerOf(c)
	if model == "" {
		if len(p.Models) == 0 {
			return ProviderTest{OK: false, Error: "the provider has no model to call; add one first"}, nil
		}
		model = p.Models[0]
	}
	mode := "chat"
	if tags, tagErr := s.settings.ModelTags(ctx); tagErr == nil && slices.Contains(tags[model], "Embedding") {
		mode = "embedding"
	}
	start := time.Now()
	res, err := s.gateway.TestConnection(ctx, route(p.Kind, model), id, mode)
	if err != nil {
		return ProviderTest{}, gatewayError(err)
	}
	return ProviderTest{OK: res.OK, Model: model, Error: res.Error, LatencyMS: int(time.Since(start).Milliseconds())}, nil
}

// perToken is a price per million tokens as LiteLLM's price per token.
func perToken(perMTok *float64) *float64 {
	if perMTok == nil {
		return nil
	}
	v := *perMTok / 1e6
	return &v
}

// errSyncFailed wraps a provider whose models could not be listed.
var errSyncFailed = errors.New("sync a provider's models")

func syncError(id string, err error) error { return fmt.Errorf("%w %s: %w", errSyncFailed, id, err) }
