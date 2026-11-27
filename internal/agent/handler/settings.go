package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type providerRequest struct {
	Name string `json:"-" path:"name"`
}

// ListProviders serves GET /v1/console/settings/providers.
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, _ httpx.None) (items[service.Provider], error) {
		return listOf(h.svc.Providers(r.Context()))
	})
}

// ProviderModels serves GET /v1/console/settings/providers/{name}/models.
func (h *Handler) ProviderModels(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, in providerRequest) (items[service.CatalogModel], error) {
		return listOf(h.svc.ProviderModels(r.Context(), in.Name))
	})
}

// ListModels serves GET /v1/console/settings/models.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, _ httpx.None) (service.ModelList, error) {
		return h.svc.Models(r.Context())
	})
}

type modelTagsRequest struct {
	service.ModelTags
}

// SetModelTags serves PUT /v1/console/settings/models/tags; the model is
// in the body since its id may hold a slash.
func (h *Handler) SetModelTags(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in modelTagsRequest) (service.ModelTags, error) {
		return h.svc.SetModelTags(r.Context(), c, in.ModelTags)
	})
}

type modelPriceRequest struct {
	service.ModelPrice
}

// SetModelPrice serves PUT /v1/console/settings/models/price.
func (h *Handler) SetModelPrice(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in modelPriceRequest) (service.ModelPrice, error) {
		return h.svc.SetModelPrice(r.Context(), c, in.ModelPrice)
	})
}

// ListAgents serves GET /v1/console/settings/agents.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, _ httpx.None) (items[service.AgentView], error) {
		return listOf(h.svc.Agents(r.Context()))
	})
}

type agentCreateRequest struct {
	service.NewAgent
}

// CreateAgent serves POST /v1/console/settings/agents.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusCreated, func(c authn.ConsoleMember, in agentCreateRequest) (service.AgentView, error) {
		return h.svc.CreateAgent(r.Context(), c, in.NewAgent)
	})
}

type agentPatchRequest struct {
	service.AgentPatch

	ID uuid.UUID `json:"-" path:"id"`
}

// UpdateAgent serves PATCH /v1/console/settings/agents/{id}.
func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in agentPatchRequest) (service.AgentView, error) {
		return h.svc.UpdateAgent(r.Context(), c, in.ID, in.AgentPatch)
	})
}

// AgentVersions serves GET /v1/console/settings/agents/{id}/versions.
func (h *Handler) AgentVersions(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, in idRequest) (items[service.AgentVersion], error) {
		return listOf(h.svc.AgentVersions(r.Context(), in.ID))
	})
}

type rangeRequest struct {
	Range string `json:"-" query:"range"`
}

type spendRequest struct {
	From string `json:"-" query:"from"`
	To   string `json:"-" query:"to"`
}

// GetSpend serves GET /v1/console/settings/spend.
func (h *Handler) GetSpend(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in spendRequest) (service.Spend, error) {
		return h.svc.Spend(r.Context(), c, in.From, in.To)
	})
}

// AgentUsage serves GET /v1/console/settings/agents/usage.
func (h *Handler) AgentUsage(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in rangeRequest) (items[service.AgentUsage], error) {
		return listOf(h.svc.AgentUsage(r.Context(), c, in.Range))
	})
}
