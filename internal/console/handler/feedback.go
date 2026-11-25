package handler

import (
	"net/http"
	"uuid"

	feedbackdomain "github.com/shopnexus/server/internal/feedback/domain"
	feedbacksvc "github.com/shopnexus/server/internal/feedback/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

type feedbackQueueRequest struct {
	Status string    `json:"-" query:"status"`
	Kind   string    `json:"-" query:"kind"`
	Cursor uuid.UUID `json:"-" query:"cursor"`
	Limit  int       `json:"-" query:"limit"`
}

// ListFeedback serves GET /v1/console/feedback.
func (h *Handler) ListFeedback(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in feedbackQueueRequest) (page.Page[feedbacksvc.ConsoleFeedback], error) {
		var status *feedbackdomain.Status
		if in.Status != "" {
			status = new(feedbackdomain.Status(in.Status))
		}
		var kind *feedbackdomain.Kind
		if in.Kind != "" {
			kind = new(feedbackdomain.Kind(in.Kind))
		}
		return h.feedback.Queue(r.Context(), status, kind, page.Request{Cursor: in.Cursor, Limit: in.Limit})
	})
}

type feedbackRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetFeedback serves GET /v1/console/feedback/{id}.
func (h *Handler) GetFeedback(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in feedbackRequest) (feedbacksvc.ConsoleFeedbackDetail, error) {
		return h.feedback.Detail(r.Context(), in.ID)
	})
}

// feedbackGroups is the list of groups ListFeedbackGroups answers.
type feedbackGroups struct {
	Items []feedbacksvc.GroupView `json:"items"`
}

type feedbackGroupsRequest struct {
	Kind string `json:"-" query:"kind"`
}

// ListFeedbackGroups serves GET /v1/console/feedback/groups.
func (h *Handler) ListFeedbackGroups(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in feedbackGroupsRequest) (feedbackGroups, error) {
		var kind *feedbackdomain.Kind
		if in.Kind != "" {
			kind = new(feedbackdomain.Kind(in.Kind))
		}
		out, err := h.feedback.Groups(r.Context(), kind)
		return feedbackGroups{out}, err
	})
}

// FeedbackSummary serves GET /v1/console/feedback/summary.
func (h *Handler) FeedbackSummary(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(_ httpx.None) (feedbacksvc.SummaryView, error) {
		return h.feedback.Summary(r.Context())
	})
}

type feedbackStatusRequest struct {
	ID      uuid.UUID `json:"-"       path:"id"`
	Status  string    `json:"status"`
	Note    string    `json:"note"`
	Version string    `json:"version"`
}

// SetFeedbackStatus serves POST /v1/console/feedback/{id}/status.
func (h *Handler) SetFeedbackStatus(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in feedbackStatusRequest) (feedbacksvc.ConsoleFeedback, error) {
		return h.feedback.SetStatus(r.Context(), in.ID, feedbacksvc.StatusInput{Status: in.Status, Note: in.Note, Version: in.Version},
			audit.ByMember(caller))
	})
}

type feedbackReplyRequest struct {
	ID   uuid.UUID `json:"-"    path:"id"`
	Text string    `json:"text"`
}

// ReplyFeedback serves POST /v1/console/feedback/{id}/reply.
func (h *Handler) ReplyFeedback(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in feedbackReplyRequest) (feedbacksvc.ConsoleFeedback, error) {
		return h.feedback.Reply(r.Context(), in.ID, in.Text, audit.ByMember(caller))
	})
}

// HeartFeedback serves PUT /v1/console/feedback/{id}/heart.
func (h *Handler) HeartFeedback(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in feedbackRequest) (feedbacksvc.ConsoleFeedback, error) {
		return h.feedback.Heart(r.Context(), in.ID, true, audit.ByMember(caller))
	})
}

// UnheartFeedback serves DELETE /v1/console/feedback/{id}/heart.
func (h *Handler) UnheartFeedback(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in feedbackRequest) (feedbacksvc.ConsoleFeedback, error) {
		return h.feedback.Heart(r.Context(), in.ID, false, audit.ByMember(caller))
	})
}

type feedbackSameAsRequest struct {
	ID         uuid.UUID `json:"-"           path:"id"`
	FeedbackID uuid.UUID `json:"feedback_id"`
}

// JoinFeedback serves POST /v1/console/feedback/{id}/same-as.
func (h *Handler) JoinFeedback(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in feedbackSameAsRequest) (feedbacksvc.ConsoleFeedback, error) {
		return h.feedback.SameAs(r.Context(), in.ID, in.FeedbackID, audit.ByMember(caller))
	})
}
