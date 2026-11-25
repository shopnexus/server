package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/provider/litellm"
)

// SyncEvery is how often the providers in sync mode have their models
// listed.
const SyncEvery = 6 * time.Hour

// SyncProviders lists the models of every provider in sync mode and adds
// the new ones to its list, with no tags until an admin sets them. A
// provider that fails is skipped and named in the error; the others go on.
// Two replicas that sync at once may both add a model; the next sync keeps
// one of the two deployments.
func (s *Agent) SyncProviders(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 4*gatewayTimeout)
	defer cancel()
	creds, err := s.d.Gateway.Credentials(ctx)
	if err != nil {
		return gatewayError(err)
	}
	var errs []error
	for _, c := range creds {
		if infoString(c.Info, infoMode) != string(domain.ModeSync) {
			continue
		}
		if _, err = s.syncProvider(ctx, (state{}).providerOf(c)); err != nil {
			errs = append(errs, syncError(c.Name, err))
		}
	}
	return errors.Join(errs...)
}

// syncProvider gives p its lister, adds each model the lister lists that
// p's list lacks, drops a model's second catalog deployment, and stamps
// synced_at.
func (s *Agent) syncProvider(ctx context.Context, p Provider) (Provider, error) {
	st, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	if st, err = s.ensureLister(ctx, st, p); err != nil {
		return Provider{}, err
	}
	have := map[string]bool{}
	for _, d := range st.deps {
		if role(d) != roleCatalog || d.InfoString("provider") != p.ID {
			continue
		}
		model := d.InfoString("model")
		if have[model] {
			if err = s.d.Gateway.DeleteDeployment(ctx, d.ID()); err != nil {
				return Provider{}, gatewayError(err)
			}
			continue
		}
		have[model] = true
	}
	for _, d := range st.deps {
		model, ok := strings.CutPrefix(d.ModelName, p.ID+"/")
		if !ok || model == "*" || model == "" || role(d) != roleLister || have[model] {
			continue
		}
		if err = s.d.Gateway.AddDeployment(ctx, catalogDeployment(p, model, litellm.Params{})); err != nil {
			return Provider{}, gatewayError(err)
		}
		have[model] = true
	}
	now := s.d.Now().UTC().Format(time.RFC3339)
	if err = s.d.Gateway.UpdateCredential(ctx, litellm.Credential{
		Name: p.ID, DisplayName: p.Name, Info: map[string]any{infoSyncedAt: now},
	}); err != nil {
		return Provider{}, gatewayError(err)
	}
	if st, err = s.readState(ctx); err != nil {
		return Provider{}, err
	}
	c, _ := st.credential(p.ID)
	return st.providerOf(c), nil
}
