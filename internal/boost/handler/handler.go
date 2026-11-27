// Package handler serves module boost's HTTP API: the packages, a
// seller's purchases, and a listing's boosts for the console.
package handler

import (
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/boost/service"
	paymentdomain "github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the packages open to anyone, the purchases behind protect
// and the console's read behind moderator.
func (h *Handler) Register(r *httpx.Router, protect, moderator func(http.Handler) http.Handler) {
	r.Handle("GET /v1/boost-packages", http.HandlerFunc(h.Packages))
	r.Handle("POST /v1/boosts", protect(http.HandlerFunc(h.Buy)))
	r.Handle("GET /v1/boosts/{id}", protect(http.HandlerFunc(h.Get)))
	r.Handle("GET /v1/console/listings/{id}/boosts", moderator(http.HandlerFunc(h.ConsoleList)))
}

type packageJSON struct {
	ID    string `json:"id"`
	Days  int    `json:"days"`
	Price int64  `json:"price"`
}

type packageList struct {
	Items     []packageJSON `json:"items"`
	Available bool          `json:"available"`
}

func (h *Handler) Packages(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(httpx.None) (packageList, error) {
		ps := h.svc.Packages()
		out := packageList{Items: make([]packageJSON, len(ps.Items)), Available: ps.Available}
		for i, p := range ps.Items {
			out.Items[i] = packageJSON{ID: p.ID, Days: p.Days, Price: p.Price}
		}
		return out, nil
	})
}

type buyRequest struct {
	Key       uuid.UUID `header:"Idempotency-Key" json:"-"`
	ListingID uuid.UUID `json:"listing_id"`
	Package   string    `json:"package"`
}

// Buy answers 201 for a new boost and 200 for one it found.
func (h *Handler) Buy(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in buyRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, created, err := h.svc.Buy(r.Context(), service.BuyInput{
		Seller: p.AccountID, ListingID: in.ListingID, Package: in.Package, Key: in.Key,
		IP: httpx.ClientIP(r).String(), English: locale.Of(string(locale.FromRequest(r))) == locale.English,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.Respond(w, status, v)
}

type idPath struct {
	ID uuid.UUID `json:"-" path:"id"`
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in idPath) (service.View, error) {
		return h.svc.Get(r.Context(), p.AccountID, in.ID)
	})
}

type consoleListRequest struct {
	page.Request

	ID uuid.UUID `json:"-" path:"id"`
}

// consolePayment is a payment as ConsolePayment shows it.
type consolePayment struct {
	ID         uuid.UUID            `json:"id"`
	Gateway    string               `json:"gateway"`
	Amount     int64                `json:"amount"`
	Status     paymentdomain.Status `json:"status"`
	GatewayRef string               `json:"gateway_ref,omitempty"`
	BankCode   string               `json:"bank_code,omitempty"`
	ResultCode string               `json:"result_code,omitempty"`
	CreatedAt  time.Time            `json:"created_at"`
	ExpiresAt  time.Time            `json:"expires_at"`
	PaidAt     *time.Time           `json:"paid_at,omitempty"`
	ClosedAt   *time.Time           `json:"closed_at,omitempty"`
}

type consoleBoost struct {
	Boost   service.View   `json:"boost"`
	Payment consolePayment `json:"payment"`
}

func (h *Handler) ConsoleList(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, in consoleListRequest) (page.Page[consoleBoost], error) {
		got, err := h.svc.ListingBoosts(r.Context(), in.ID, in.Request)
		if err != nil {
			return page.Page[consoleBoost]{}, err
		}
		out := page.Page[consoleBoost]{Items: make([]consoleBoost, len(got.Items)), NextCursor: got.NextCursor}
		for i := range got.Items {
			c := &got.Items[i]
			p := c.Payment
			out.Items[i] = consoleBoost{Boost: c.Boost, Payment: consolePayment{
				ID: p.ID, Gateway: p.Gateway, Amount: p.Amount, Status: p.Status, GatewayRef: p.GatewayRef, BankCode: p.BankCode,
				ResultCode: p.ResultCode, CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt, PaidAt: p.PaidAt, ClosedAt: p.ClosedAt,
			}}
		}
		return out, nil
	})
}
