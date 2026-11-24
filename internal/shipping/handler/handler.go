// Package handler serves module shipping's HTTP API: the seller's carrier
// accounts, the carrier's pickup points, a shipment's quote, and the
// carriers' webhooks.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/domain"
	"github.com/shopnexus/server/internal/shipping/service"
)

type Handler struct {
	svc     service.Service
	secrets map[carrier.Carrier]string
	log     *slog.Logger
}

// New takes the webhook secret of each carrier; a carrier without one, or
// with an empty one, has no webhook.
func New(svc service.Service, secrets map[carrier.Carrier]string, log *slog.Logger) *Handler {
	return &Handler{svc: svc, secrets: secrets, log: log}
}

// Register adds the account, pickup and quote endpoints behind protect, and
// the webhooks open to the carriers.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("PUT /v1/me/shipping-settings", protect(http.HandlerFunc(h.PutSettings)))
	r.Handle("GET /v1/carrier-accounts", protect(http.HandlerFunc(h.List)))
	r.Handle("PUT /v1/carrier-accounts/{carrier}", protect(http.HandlerFunc(h.Link)))
	r.Handle("DELETE /v1/carrier-accounts/{carrier}", protect(http.HandlerFunc(h.Unlink)))
	r.Handle("POST /v1/carrier-accounts/{carrier}/otp", protect(http.HandlerFunc(h.SendOTP)))
	r.Handle("GET /v1/carrier-accounts/{carrier}/pickups", protect(http.HandlerFunc(h.Pickups)))
	r.Handle("GET /v1/shipments/quote", protect(http.HandlerFunc(h.Quote)))
	r.Handle("POST /v1/carriers/{carrier}/webhook/{secret}", http.HandlerFunc(h.Webhook))
}

type carrierPath struct {
	Carrier string `json:"-" path:"carrier"`
}

type linkRequest struct {
	Carrier  string `json:"-"                  path:"carrier"`
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	ShopID   string `json:"shop_id,omitempty"`
	Phone    string `json:"phone,omitempty"`
	OTP      string `json:"otp,omitempty"`
}

type otpRequest struct {
	Carrier string `json:"-"     path:"carrier"`
	Phone   string `json:"phone"`
}

type quoteRequest struct {
	SavedPickup   bool      `json:"-" query:"saved_pickup"`
	OrderID       uuid.UUID `json:"-" query:"order_id"`
	Carrier       string    `json:"-" query:"carrier"`
	Service       string    `json:"-" query:"service"`
	WeightGrams   int       `json:"-" query:"weight_grams"`
	PickupPointID string    `json:"-" query:"carrier_pickup_id"`
	AddressID     uuid.UUID `json:"-" query:"address_id"`
}

type accountList struct {
	Items []service.AccountView `json:"items"`
}

type pickupPoint struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
}

type pickupList struct {
	Items []pickupPoint `json:"items"`
}

type quote struct {
	Fee int64 `json:"fee"`
}

// List serves GET /v1/carrier-accounts.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	views, err := h.svc.Accounts(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, accountList{Items: views})
}

// Link serves PUT /v1/carrier-accounts/{carrier}. GHTK takes an email and
// a password, GHN a token and a ShopId or a phone and its code; the other
// carrier's body is invalid_carrier.
func (h *Handler) Link(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in linkRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	ghtkBody := in.Email != "" || in.Password != ""
	ghnBody := in.Token != "" || in.ShopID != "" || in.Phone != "" || in.OTP != ""
	if (in.Carrier == string(carrier.GHTK) && (!ghtkBody || ghnBody)) || (in.Carrier == string(carrier.GHN) && ghtkBody) {
		httpx.WriteError(w, r, h.log, domain.ErrInvalidCarrier.WithField("body", apperr.NotAllowed, "carrier"))
		return
	}
	v, err := h.svc.Link(r.Context(), p.AccountID, in.Carrier,
		carrier.Credentials{Email: in.Email, Password: in.Password, Token: in.Token, ShopID: in.ShopID, Phone: in.Phone, OTP: in.OTP})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// SendOTP serves POST /v1/carrier-accounts/{carrier}/otp.
func (h *Handler) SendOTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := httpx.Caller(w, r, h.log); !ok {
		return
	}
	var in otpRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.SendOTP(r.Context(), in.Carrier, in.Phone); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Unlink serves DELETE /v1/carrier-accounts/{carrier}.
func (h *Handler) Unlink(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in carrierPath
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Unlink(r.Context(), p.AccountID, in.Carrier); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Pickups serves GET /v1/carrier-accounts/{carrier}/pickups.
func (h *Handler) Pickups(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in carrierPath
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	pts, err := h.svc.Pickups(r.Context(), p.AccountID, in.Carrier)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out := pickupList{Items: make([]pickupPoint, 0, len(pts))}
	for _, pt := range pts {
		out.Items = append(out.Items, pickupPoint{ID: pt.ID, Name: pt.Name, Phone: pt.Phone, Address: pt.Address})
	}
	httpx.Respond(w, http.StatusOK, out)
}

// Quote serves GET /v1/shipments/quote.
func (h *Handler) Quote(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in quoteRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	fee, err := h.svc.Quote(r.Context(), p.AccountID, in.Carrier, service.QuoteRequest{
		SavedPickup: in.SavedPickup, OrderID: in.OrderID, Service: in.Service, WeightGrams: in.WeightGrams, PickupPointID: in.PickupPointID, AddressID: in.AddressID,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, quote{Fee: fee})
}
