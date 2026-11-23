package handler

import (
	"net/http"
	"uuid"

	feedbackdomain "github.com/shopnexus/server/internal/feedback/domain"
	feedbacksvc "github.com/shopnexus/server/internal/feedback/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

type feedbackQueueRequest struct {
	Status string    `json:"-" query:"status"`
	Cursor uuid.UUID `json:"-" query:"cursor"`
	Limit  int       `json:"-" query:"limit"`
}

// ListFeedback serves GET /v1/console/feedback.
func (h *Handler) ListFeedback(w http.ResponseWriter, r *http.Request) {
	var in feedbackQueueRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	var status *feedbackdomain.Status
	if in.Status != "" {
		status = new(feedbackdomain.Status(in.Status))
	}
	out, err := h.feedback.Queue(r.Context(), status, page.Request{Cursor: in.Cursor, Limit: in.Limit})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type feedbackStatusRequest struct {
	ID      uuid.UUID `json:"-"       path:"id"`
	Status  string    `json:"status"`
	Note    string    `json:"note"`
	Version string    `json:"version"`
}

// SetFeedbackStatus serves POST /v1/console/feedback/{id}/status.
func (h *Handler) SetFeedbackStatus(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in feedbackStatusRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.feedback.SetStatus(r.Context(), in.ID, feedbacksvc.StatusInput{Status: in.Status, Note: in.Note, Version: in.Version},
		audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type feedbackReplyRequest struct {
	ID   uuid.UUID `json:"-"    path:"id"`
	Text string    `json:"text"`
}

// ReplyFeedback serves POST /v1/console/feedback/{id}/reply.
func (h *Handler) ReplyFeedback(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in feedbackReplyRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.feedback.Reply(r.Context(), in.ID, in.Text, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type feedbackRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// HeartFeedback serves PUT /v1/console/feedback/{id}/heart.
func (h *Handler) HeartFeedback(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in feedbackRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.feedback.Heart(r.Context(), in.ID, true, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// UnheartFeedback serves DELETE /v1/console/feedback/{id}/heart.
func (h *Handler) UnheartFeedback(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in feedbackRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.feedback.Heart(r.Context(), in.ID, false, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type feedbackSameAsRequest struct {
	ID         uuid.UUID `json:"-"           path:"id"`
	FeedbackID uuid.UUID `json:"feedback_id"`
}

// JoinFeedback serves POST /v1/console/feedback/{id}/same-as.
func (h *Handler) JoinFeedback(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in feedbackSameAsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.feedback.SameAs(r.Context(), in.ID, in.FeedbackID, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}
