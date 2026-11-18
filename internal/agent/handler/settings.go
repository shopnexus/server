package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
)

type providerRequest struct {
	ID string `json:"-" path:"id"`
}

// ListProviders serves GET /v1/console/settings/providers.
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(_ authn.ConsoleMember, _ none) (items[service.Provider], error) {
		return listOf(h.svc.Providers(r.Context()))
	})
}

type providerCreateRequest struct {
	service.NewProvider
}

// CreateProvider serves POST /v1/console/settings/providers.
func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusCreated, func(c authn.ConsoleMember, in providerCreateRequest) (service.Provider, error) {
		return h.svc.CreateProvider(r.Context(), c, in.NewProvider)
	})
}

type providerPatchRequest struct {
	service.ProviderPatch

	ID string `json:"-" path:"id"`
}

// UpdateProvider serves PATCH /v1/console/settings/providers/{id}.
func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(c authn.ConsoleMember, in providerPatchRequest) (service.Provider, error) {
		return h.svc.UpdateProvider(r.Context(), c, in.ID, in.ProviderPatch)
	})
}

// DeleteProvider serves DELETE /v1/console/settings/providers/{id}.
func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusNoContent, func(c authn.ConsoleMember, in providerRequest) (none, error) {
		return none{}, h.svc.DeleteProvider(r.Context(), c, in.ID)
	})
}

type providerModelRequest struct {
	ID         string   `json:"-"                              path:"id"`
	Model      string   `json:"model"`
	InPerMTok  *float64 `json:"input_cost_per_mtok,omitempty"`
	OutPerMTok *float64 `json:"output_cost_per_mtok,omitempty"`
}

// AddProviderModel serves POST /v1/console/settings/providers/{id}/models.
func (h *Handler) AddProviderModel(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(c authn.ConsoleMember, in providerModelRequest) (service.Provider, error) {
		return h.svc.AddProviderModel(r.Context(), c, in.ID, in.Model, in.InPerMTok, in.OutPerMTok)
	})
}

type providerModelPathRequest struct {
	ID    string `json:"-" path:"id"`
	Model string `json:"-" query:"model"`
}

// RemoveProviderModel serves DELETE
// /v1/console/settings/providers/{id}/models?model=, the model in the
// query since it may hold a slash.
func (h *Handler) RemoveProviderModel(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(c authn.ConsoleMember, in providerModelPathRequest) (service.Provider, error) {
		return h.svc.RemoveProviderModel(r.Context(), c, in.ID, in.Model)
	})
}

type providerTestRequest struct {
	ID    string `json:"-"               path:"id"`
	Model string `json:"model,omitempty"`
}

// TestProvider serves POST /v1/console/settings/providers/{id}/test.
func (h *Handler) TestProvider(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(_ authn.ConsoleMember, in providerTestRequest) (service.ProviderTest, error) {
		return h.svc.TestProvider(r.Context(), in.ID, in.Model)
	})
}

// ListModels serves GET /v1/console/settings/models.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(_ authn.ConsoleMember, _ none) (service.ModelList, error) {
		return h.svc.Models(r.Context())
	})
}

type modelTagsRequest struct {
	Model string   `json:"model"`
	Tags  []string `json:"tags"`
}

type modelTags struct {
	Model string   `json:"model"`
	Tags  []string `json:"tags"`
}

// tagsOf is a model's tags as the response.
func tagsOf(model string) func([]string, error) (modelTags, error) {
	return func(tags []string, err error) (modelTags, error) { return modelTags{model, tags}, err }
}

// SetModelTags serves PUT /v1/console/settings/models/tags.
func (h *Handler) SetModelTags(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(c authn.ConsoleMember, in modelTagsRequest) (modelTags, error) {
		return tagsOf(in.Model)(h.svc.SetModelTags(r.Context(), c, in.Model, in.Tags))
	})
}

// ListAgents serves GET /v1/console/settings/agents.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(_ authn.ConsoleMember, _ none) (items[service.AgentView], error) {
		return listOf(h.svc.Agents(r.Context()))
	})
}

type agentCreateRequest struct {
	service.NewAgent
}

// CreateAgent serves POST /v1/console/settings/agents.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusCreated, func(c authn.ConsoleMember, in agentCreateRequest) (service.AgentView, error) {
		return h.svc.CreateAgent(r.Context(), c, in.NewAgent)
	})
}

type agentPatchRequest struct {
	service.AgentPatch

	ID uuid.UUID `json:"-" path:"id"`
}

// UpdateAgent serves PATCH /v1/console/settings/agents/{id}.
func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(c authn.ConsoleMember, in agentPatchRequest) (service.AgentView, error) {
		return h.svc.UpdateAgent(r.Context(), c, in.ID, in.AgentPatch)
	})
}

// AgentVersions serves GET /v1/console/settings/agents/{id}/versions.
func (h *Handler) AgentVersions(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(_ authn.ConsoleMember, in idRequest) (items[service.AgentVersion], error) {
		return listOf(h.svc.AgentVersions(r.Context(), in.ID))
	})
}

// GetRotation serves GET /v1/console/settings/rotation.
func (h *Handler) GetRotation(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(_ authn.ConsoleMember, _ none) (service.Rotation, error) {
		return h.svc.Rotation(r.Context())
	})
}

type rotationRequest struct {
	service.RotationPatch
}

// UpdateRotation serves PATCH /v1/console/settings/rotation.
func (h *Handler) UpdateRotation(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(c authn.ConsoleMember, in rotationRequest) (service.Rotation, error) {
		return h.svc.UpdateRotation(r.Context(), c, in.RotationPatch)
	})
}

// GetBudgets serves GET /v1/console/settings/budgets.
func (h *Handler) GetBudgets(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(_ authn.ConsoleMember, _ none) (service.Budgets, error) {
		return h.svc.Budgets(r.Context())
	})
}

type budgetsRequest struct {
	service.BudgetsPatch
}

// UpdateBudgets serves PATCH /v1/console/settings/budgets.
func (h *Handler) UpdateBudgets(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(c authn.ConsoleMember, in budgetsRequest) (service.Budgets, error) {
		return h.svc.UpdateBudgets(r.Context(), c, in.BudgetsPatch)
	})
}

type spendRequest struct {
	Range string `json:"-" query:"range"`
}

// GetSpend serves GET /v1/console/settings/spend.
func (h *Handler) GetSpend(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, http.StatusOK, func(_ authn.ConsoleMember, in spendRequest) (service.Spend, error) {
		return h.svc.Spend(r.Context(), in.Range)
	})
}
