package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/service"
)

type reportRequest struct {
	ListingID      uuid.UUID `json:"listing_id"`
	AccountID      uuid.UUID `json:"account_id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	Reason         string    `json:"reason"`
	Note           string    `json:"note"`
}

type reportIDRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// Report serves POST /v1/reports: 201 for a new report, 200 for the open
// one updated.
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in reportRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, added, err := h.svc.Report(r.Context(), p.AccountID, service.ReportInput{
		ListingID: in.ListingID, AccountID: in.AccountID, ConversationID: in.ConversationID, Reason: domain.Reason(in.Reason), Note: in.Note,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	status := http.StatusOK
	if added {
		status = http.StatusCreated
	}
	httpx.Respond(w, status, v)
}

// WithdrawReport serves DELETE /v1/reports/{id}.
func (h *Handler) WithdrawReport(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in reportIDRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.WithdrawReport(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
