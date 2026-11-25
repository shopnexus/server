// Package handler serves module chotot's HTTP API.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/chotot/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the chotot endpoints; protect requires a signed-in caller.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("POST /v1/me/chotot/codes", protect(http.HandlerFunc(h.Code)))
	r.Handle("GET /v1/me/chotot", protect(http.HandlerFunc(h.Get)))
	r.Handle("PUT /v1/me/chotot", protect(http.HandlerFunc(h.Link)))
	r.Handle("PATCH /v1/me/chotot", protect(http.HandlerFunc(h.Update)))
	r.Handle("DELETE /v1/me/chotot", protect(http.HandlerFunc(h.Unlink)))
	r.Handle("GET /v1/me/chotot/ads", protect(http.HandlerFunc(h.Ads)))
	r.Handle("POST /v1/me/chotot/imports", protect(http.HandlerFunc(h.Import)))
	r.Handle("POST /v1/me/chotot/ads/{list_id}/listing", protect(http.HandlerFunc(h.PostDraft)))
}

// Code serves POST /v1/me/chotot/codes.
func (h *Handler) Code(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (domain.Code, error) {
		return h.svc.Code(r.Context(), p.AccountID)
	})
}

// Get serves GET /v1/me/chotot.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.StateView, error) {
		return h.svc.Get(r.Context(), p.AccountID)
	})
}

type linkRequest struct {
	ProfileURL string `json:"profile_url"`
}

// Link serves PUT /v1/me/chotot.
func (h *Handler) Link(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in linkRequest) (service.LinkView, error) {
		return h.svc.Link(r.Context(), p.AccountID, in.ProfileURL)
	})
}

// Update serves PATCH /v1/me/chotot.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in domain.SettingsPatch) (service.LinkView, error) {
		return h.svc.Update(r.Context(), p.AccountID, in)
	})
}

// Unlink serves DELETE /v1/me/chotot.
func (h *Handler) Unlink(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, _ httpx.None) (httpx.None, error) {
		return httpx.None{}, h.svc.Unlink(r.Context(), p.AccountID)
	})
}

// Ads serves GET /v1/me/chotot/ads.
func (h *Handler) Ads(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.AdListView, error) {
		return h.svc.Ads(r.Context(), p.AccountID)
	})
}

type importRequest struct {
	ListIDs []int64 `json:"list_ids"`
}

// Import serves POST /v1/me/chotot/imports.
func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusAccepted, func(p authn.Principal, in importRequest) (service.ImportListView, error) {
		return h.svc.Import(r.Context(), p.AccountID, in.ListIDs)
	})
}

// draftRequest is the ListingInput schema of api/schemas/catalog.yaml for
// the listing on Chợ Tốt list_id. A GPS point is not taken: the listing's
// commune places it.
type draftRequest struct {
	ListID       int64                   `json:"-"             path:"list_id"`
	Title        string                  `json:"title"`
	Price        int64                   `json:"price"`
	Description  string                  `json:"description"`
	CategoryCode string                  `json:"category_code"`
	Condition    catalogdomain.Condition `json:"condition"`
	AllowOffers  bool                    `json:"allow_offers"`
	CommuneCode  string                  `json:"commune_code"`
	Photos       []uuid.UUID             `json:"photos"`
	Specs        []catalogdomain.Spec    `json:"specs"`
}

// PostDraft serves POST /v1/me/chotot/ads/{list_id}/listing.
func (h *Handler) PostDraft(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in draftRequest) (service.ImportView, error) {
		return h.svc.PostDraft(r.Context(), p.AccountID, in.ListID, catalogdomain.ListingInput{
			Title: in.Title, Price: in.Price, Description: in.Description, CategoryCode: in.CategoryCode,
			Condition: in.Condition, AllowOffers: in.AllowOffers, CommuneCode: in.CommuneCode, Photos: in.Photos, Specs: in.Specs,
		})
	})
}
