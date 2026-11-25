package handler

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/domain"
)

// maxReport caps a report's body; GHTK's and GHN's are a few hundred bytes.
const maxReport = 64 << 10

var errNoRoute = apperr.New(apperr.KindNotFound, "not_found", "no such route")

// Webhook serves POST /v1/carriers/{carrier}/webhook/{secret}. The secret
// in the path is the only check: GHTK signs nothing. Once it matches, the
// answer is 200 whatever the report says, a code no order has included,
// since GHTK sends again on anything else and the poll covers a report
// lost here.
func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	c := carrier.Carrier(r.PathValue("carrier"))
	want := h.secrets[c]
	if want == "" || subtle.ConstantTimeCompare([]byte(want), []byte(r.PathValue("secret"))) != 1 {
		httpx.WriteError(w, r, h.log, errNoRoute)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxReport))
	if err == nil {
		err = h.svc.Reported(r.Context(), c, r.Header.Get("Content-Type"), body)
	}
	if errors.Is(err, domain.ErrReportUnread) || (err != nil && body == nil) {
		h.log.WarnContext(r.Context(), "carrier report not read", "carrier", c, "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type devReport struct {
	Code         string       `json:"-"             path:"code"`
	Step         carrier.Step `json:"step"`
	Reason       string       `json:"reason"`
	Warehouse    string       `json:"warehouse"`
	ShipperName  string       `json:"shipper_name"`
	ShipperPhone string       `json:"shipper_phone"`
	ProofURL     string       `json:"proof_url"`
}

// DevReport serves POST /v1/dev/shipments/{code}/status, which Register
// adds only for the fake carrier: a step of the parcel's, as its carrier
// would report it now.
func (h *Handler) DevReport(w http.ResponseWriter, r *http.Request) {
	var in devReport
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	ev := carrier.Event{
		Kind: carrier.KindStep, Step: in.Step, CarrierStatus: string(in.Step), Reason: in.Reason,
		Warehouse: in.Warehouse, ShipperName: in.ShipperName, ShipperPhone: in.ShipperPhone, ProofURL: in.ProofURL,
	}
	if err := h.svc.DevReported(r.Context(), in.Code, ev); err != nil {
		httpx.WriteError(w, r, h.log, fmt.Errorf("dev report: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
