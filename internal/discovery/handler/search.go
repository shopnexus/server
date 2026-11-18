package handler

import (
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// searchRequest is a search as the query string gives it. The position
// reaches the service and no log.
type searchRequest struct {
	NearLat     float64 `json:"-" query:"near_lat"`
	NearLng     float64 `json:"-" query:"near_lng"`
	NearCommune string  `json:"-" query:"near_commune"`

	Query      string                    `json:"-" query:"q"`
	Category   string                    `json:"-" query:"category"`
	PriceMin   int64                     `json:"-" query:"price_min"`
	PriceMax   int64                     `json:"-" query:"price_max"`
	Conditions []catalogdomain.Condition `json:"-" query:"condition"`
	RadiusKm   int                       `json:"-" query:"radius_km"`
	OffersOnly bool                      `json:"-" query:"offers_only"`
	Sort       catalogdomain.SortOrder   `json:"-" query:"sort"`
	Off        []string                  `json:"-" query:"off"`
	Cursor     string                    `json:"-" query:"cursor"`
	Limit      int                       `json:"-" query:"limit"`
	Photo      uuid.UUID                 `json:"-" query:"photo"`
	Like       uuid.UUID                 `json:"-" query:"like"`
	Watch      uuid.UUID                 `json:"-" query:"watch"`
}

// Search serves GET /v1/search, signed in or not.
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	var in searchRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Search(r.Context(), httpx.Viewer(r), service.SearchInput{
		Query: in.Query, Category: in.Category, PriceMin: in.PriceMin, PriceMax: in.PriceMax, Conditions: in.Conditions,
		RadiusKm: in.RadiusKm, OffersOnly: in.OffersOnly, Sort: in.Sort, Off: in.Off,
		Near: catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune}, Cursor: in.Cursor, Limit: in.Limit, Photo: in.Photo, Like: in.Like, Watch: in.Watch,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}
