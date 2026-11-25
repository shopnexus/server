package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/assist/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// priceGuideRequest is PriceGuideRequest; the request validator holds the
// shape rules.
type priceGuideRequest struct {
	Title        string                  `json:"title"`
	CategoryCode string                  `json:"category_code"`
	Condition    catalogdomain.Condition `json:"condition"`
	Specs        []catalogdomain.Spec    `json:"specs"`
	Description  string                  `json:"description"`
	CommuneCode  string                  `json:"commune_code"`
	Photos       []uuid.UUID             `json:"photos"`
}

// PriceGuide serves POST /v1/price-guides.
func (h *Handler) PriceGuide(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in priceGuideRequest) (service.PriceGuideView, error) {
		return h.svc.PriceGuide(r.Context(), p.AccountID, service.PriceGuideInput{
			Title: in.Title, CategoryCode: in.CategoryCode, Condition: in.Condition, Specs: in.Specs,
			Description: in.Description, CommuneCode: in.CommuneCode, Photos: in.Photos,
		})
	})
}
