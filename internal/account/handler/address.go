package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type addressRequest struct {
	Name        string             `json:"name"`
	Phone       string             `json:"phone"`
	Street      string             `json:"street"`
	CommuneCode string             `json:"commune_code"`
	Kind        domain.AddressKind `json:"kind"`
	IsDefault   bool               `json:"is_default"`
}

func (a addressRequest) draft() domain.AddressDraft {
	return domain.AddressDraft{Name: a.Name, Phone: a.Phone, Street: a.Street, CommuneCode: a.CommuneCode, Kind: a.Kind}
}

type addressCreateRequest struct {
	addressRequest

	Key uuid.UUID `header:"Idempotency-Key" json:"-"`
}

type addressUpdateRequest struct {
	addressRequest

	ID uuid.UUID `json:"-" path:"id"`
}

type addressIDRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

type addressList struct {
	Items []service.AddressView `json:"items"`
}

// Addresses serves GET /v1/addresses.
func (h *Handler) Addresses(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (addressList, error) {
		out, err := h.svc.Addresses(r.Context(), p.AccountID)
		return addressList{Items: out}, err
	})
}

// CreateAddress serves POST /v1/addresses.
func (h *Handler) CreateAddress(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusCreated, func(p authn.Principal, in addressCreateRequest) (service.AddressView, error) {
		return h.svc.CreateAddress(r.Context(), p.AccountID, in.Key, in.draft(), in.IsDefault)
	})
}

// UpdateAddress serves PUT /v1/addresses/{id}.
func (h *Handler) UpdateAddress(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in addressUpdateRequest) (service.AddressView, error) {
		return h.svc.UpdateAddress(r.Context(), p.AccountID, in.ID, in.draft(), in.IsDefault)
	})
}

// SetDefaultAddress serves POST /v1/addresses/{id}/default.
func (h *Handler) SetDefaultAddress(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in addressIDRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.SetDefaultAddress(r.Context(), p.AccountID, in.ID)
	})
}

// DeleteAddress serves DELETE /v1/addresses/{id}.
func (h *Handler) DeleteAddress(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in addressIDRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.DeleteAddress(r.Context(), p.AccountID, in.ID)
	})
}
