package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
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
	httpx.Serve(w, r, h.log, http.StatusOK, func(in listReportCasesRequest) (trustsvc.CaseList, error) {
		return h.trust.ReportCases(r.Context(), in.Status, in.Request)
	})
}

// GetReportCase serves GET /v1/console/reports/{account_id}.
func (h *Handler) GetReportCase(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in reportCaseRequest) (trustsvc.CaseDetail, error) {
		return h.trust.ReportCase(r.Context(), in.AccountID)
	})
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
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in reportCaseRequest) (trustsvc.CaseDetail, error) {
		return h.trust.ResolveReportCase(r.Context(), in.AccountID, outcome, audit.ByMember(caller))
	})
}

// ReopenReportCase serves POST /v1/console/reports/{account_id}/reopen.
func (h *Handler) ReopenReportCase(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in reportCaseRequest) (trustsvc.CaseDetail, error) {
		return h.trust.ReopenReportCase(r.Context(), in.AccountID, audit.ByMember(caller))
	})
}
