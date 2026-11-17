package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
	trustdomain "github.com/shopnexus/server2/internal/trust/domain"
)

type listReportCasesRequest struct {
	page.Request

	// Status is open or resolved; the spec fills in open.
	Status trustdomain.CaseStatus `json:"-" query:"status"`
}

type reportCaseRequest struct {
	AccountID uuid.UUID `json:"-" path:"account_id"`
}

// ListReportCases serves GET /v1/console/reports.
func (h *Handler) ListReportCases(w http.ResponseWriter, r *http.Request) {
	var in listReportCasesRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.trust.ReportCases(r.Context(), in.Status, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// GetReportCase serves GET /v1/console/reports/{account_id}.
func (h *Handler) GetReportCase(w http.ResponseWriter, r *http.Request) {
	var in reportCaseRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.trust.ReportCase(r.Context(), in.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// DismissReportCase serves POST /v1/console/reports/{account_id}/dismiss.
func (h *Handler) DismissReportCase(w http.ResponseWriter, r *http.Request) {
	h.closeReportCase(w, r, trustdomain.OutcomeDismissed)
}

// ResolveReportCase serves POST /v1/console/reports/{account_id}/resolve.
func (h *Handler) ResolveReportCase(w http.ResponseWriter, r *http.Request) {
	h.closeReportCase(w, r, trustdomain.OutcomeActioned)
}

// closeReportCase closes the account's open case with outcome.
func (h *Handler) closeReportCase(w http.ResponseWriter, r *http.Request, outcome trustdomain.Outcome) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in reportCaseRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.trust.ResolveReportCase(r.Context(), in.AccountID, outcome, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// ReopenReportCase serves POST /v1/console/reports/{account_id}/reopen.
func (h *Handler) ReopenReportCase(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in reportCaseRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.trust.ReopenReportCase(r.Context(), in.AccountID, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}
