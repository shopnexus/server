package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type addressRequest struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Street      string `json:"street"`
	CommuneCode string `json:"commune_code"`
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
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.Addresses(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, addressList{Items: out})
}

// CreateAddress serves POST /v1/addresses.
func (h *Handler) CreateAddress(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in addressCreateRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.CreateAddress(r.Context(), p.AccountID, in.Key, domain.AddressDraft{
		Name: in.Name, Phone: in.Phone, Street: in.Street, CommuneCode: in.CommuneCode,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, out)
}

// UpdateAddress serves PUT /v1/addresses/{id}.
func (h *Handler) UpdateAddress(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in addressUpdateRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.UpdateAddress(r.Context(), p.AccountID, in.ID, domain.AddressDraft{
		Name: in.Name, Phone: in.Phone, Street: in.Street, CommuneCode: in.CommuneCode,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// SetDefaultAddress serves POST /v1/addresses/{id}/default.
func (h *Handler) SetDefaultAddress(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in addressIDRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.SetDefaultAddress(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteAddress serves DELETE /v1/addresses/{id}.
func (h *Handler) DeleteAddress(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in addressIDRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.DeleteAddress(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
