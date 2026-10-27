package handler

import (
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// nearRequest is where the caller searches from. It reaches the service and
// no log.
type nearRequest struct {
	NearLat     float64 `json:"-" query:"near_lat"`
	NearLng     float64 `json:"-" query:"near_lng"`
	NearCommune string  `json:"-" query:"near_commune"`
}

func (n nearRequest) near() service.Near {
	return service.Near{Lat: n.NearLat, Lng: n.NearLng, Commune: n.NearCommune}
}

type searchRequest struct {
	nearRequest

	Query      string   `json:"-" query:"q"`
	Category   string   `json:"-" query:"category"`
	PriceMin   int64    `json:"-" query:"price_min"`
	PriceMax   int64    `json:"-" query:"price_max"`
	Conditions []string `json:"-" query:"condition"`
	RadiusKm   int      `json:"-" query:"radius_km"`
	OffersOnly bool     `json:"-" query:"offers_only"`
	Sort       string   `json:"-" query:"sort"`
	Off        []string `json:"-" query:"off"`
	Cursor     string   `json:"-" query:"cursor"`
	Limit      int      `json:"-" query:"limit"`
}

type nearbyRequest struct {
	nearRequest

	Cursor string `json:"-" query:"cursor"`
	Limit  int    `json:"-" query:"limit"`
}

// viewer is the signed-in caller, or nil.
func viewer(r *http.Request) *uuid.UUID {
	if p, ok := authn.From(r.Context()); ok {
		return &p.AccountID
	}
	return nil
}

// Search serves GET /v1/search, signed in or not.
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	var in searchRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	conditions := make([]catalogdomain.Condition, len(in.Conditions))
	for i, c := range in.Conditions {
		conditions[i] = catalogdomain.Condition(c)
	}
	out, err := h.svc.Search(r.Context(), viewer(r), service.SearchInput{
		Query: in.Query, Category: in.Category, PriceMin: in.PriceMin, PriceMax: in.PriceMax, Conditions: conditions,
		RadiusKm: in.RadiusKm, OffersOnly: in.OffersOnly, Sort: catalogdomain.SortOrder(in.Sort), Off: in.Off,
		Near: in.near(), Cursor: in.Cursor, Limit: in.Limit,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// Nearby serves GET /v1/feed/nearby, signed in or not.
func (h *Handler) Nearby(w http.ResponseWriter, r *http.Request) {
	var in nearbyRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Nearby(r.Context(), viewer(r), in.near(), in.Cursor, in.Limit)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}
