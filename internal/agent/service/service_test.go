package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/agent/domain"
	"github.com/shopnexus/server2/internal/agent/service"
	"github.com/shopnexus/server2/internal/platform/authn"
)

var caller = authn.ConsoleMember{MemberID: uuid.NewV7(), Role: "moderator"}

// agentAt builds the service on an agent served by h.
func agentAt(t *testing.T, h http.HandlerFunc) *service.Agent {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return service.New(service.Deps{BaseURL: srv.URL, Token: "service-token", HTTP: &http.Client{}, Now: time.Now})
}

// The question goes to the agent with the service token and the member's
// id and role, and the answer comes back with its sources. UC-MOD-08.
func TestAskForwardsTheMember(t *testing.T) {
	t.Parallel()
	conversation, turn, ticket := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	s := agentAt(t, func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.Method != http.MethodPost || r.URL.Path != "/conversations/"+conversation.String()+"/turns" || in.Text != "Ai mở ticket?" ||
			r.Header.Get("Authorization") != "Bearer service-token" || r.Header.Get("X-Console-Member") != caller.MemberID.String() ||
			r.Header.Get("X-Console-Role") != "moderator" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"conversation_id": conversation, "turn_id": turn, "answer": "Người mua.",
			"sources": []map[string]any{{"tool": "ticket_get", "label": "Ticket", "ids": []string{ticket.String()}}, {"tool": "account_get", "label": "Tài khoản"}},
		})
	})
	got, err := s.Ask(context.Background(), caller, conversation, "Ai mở ticket?")
	if err != nil {
		t.Fatal(err)
	}
	if got.ConversationID != conversation || got.TurnID != turn || got.Answer != "Người mua." || len(got.Sources) != 2 ||
		got.Sources[0].IDs[0] != ticket.String() || got.Sources[1].IDs == nil {
		t.Fatalf("%+v", got)
	}
}

// Each failure of the agent reaches the member as the code the console
// shows, by the agent's code, else by its status. UC-MOD-08 A1, E1.
func TestAskMapsTheAgentsFailures(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		status int
		code   string
		want   error
	}{
		{http.StatusNotFound, "not_found", domain.ErrConversationNotFound},
		{http.StatusTooManyRequests, "agent_budget", domain.ErrBudget},
		{http.StatusTooManyRequests, "rate_limited", domain.ErrRateLimited},
		{http.StatusGatewayTimeout, "agent_timeout", domain.ErrTimeout},
		{http.StatusServiceUnavailable, "agent_unavailable", domain.ErrUnavailable},
		{http.StatusBadRequest, "input_rejected", domain.ErrInputRejected},
		{http.StatusBadRequest, "bad_request", domain.ErrUnavailable},
		{http.StatusConflict, "turn_in_progress", domain.ErrTurnInProgress},
		{http.StatusConflict, "turn_limit", domain.ErrTurnLimit},
		{http.StatusConflict, "conversation_closed", domain.ErrConversationClosed},
		{http.StatusUnauthorized, "unauthorized", domain.ErrUnavailable},
		{http.StatusInternalServerError, "internal", domain.ErrUnavailable},
		{http.StatusNotFound, "", domain.ErrConversationNotFound},
		{http.StatusGatewayTimeout, "", domain.ErrTimeout},
		{http.StatusBadGateway, "", domain.ErrUnavailable},
	} {
		s := agentAt(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			if c.code != "" {
				_ = json.NewEncoder(w).Encode(map[string]string{"code": c.code})
			}
		})
		if _, err := s.Ask(context.Background(), caller, uuid.NewV7(), "?"); !errors.Is(err, c.want) {
			t.Errorf("%d %q: %v, want %v", c.status, c.code, err, c.want)
		}
	}
	s := agentAt(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) })
	if _, err := s.Ask(context.Background(), caller, uuid.NewV7(), "?"); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("a body that does not read: %v", err)
	}
	down := service.New(service.Deps{BaseURL: "http://127.0.0.1:1", Token: "t", HTTP: &http.Client{}, Now: time.Now})
	if _, err := down.Ask(context.Background(), caller, uuid.NewV7(), "?"); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("an agent that does not answer: %v", err)
	}
	stall := make(chan struct{})
	defer close(stall)
	slow := agentAt(t, func(http.ResponseWriter, *http.Request) { <-stall })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := slow.Ask(ctx, caller, uuid.NewV7(), "?"); !errors.Is(err, domain.ErrTimeout) {
		t.Errorf("a turn past its deadline: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := slow.Conversation(ctx, caller, uuid.NewV7()); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("a read past its deadline: %v", err)
	}
}

// A record's conversation names its subject to the agent; a subject id
// alone, or a new conversation with a kind alone, is refused here.
// UC-MOD-08.
func TestConversationSubjects(t *testing.T) {
	t.Parallel()
	subject := uuid.NewV7()
	var query string
	s := agentAt(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte("[]"))
			return
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": uuid.NewV7(), "subject_kind": in["subject_kind"], "subject_id": in["subject_id"], "updated_at": time.Now(),
		})
	})
	got, err := s.CreateConversation(context.Background(), caller, service.NewConversation{SubjectKind: domain.SubjectTicket, SubjectID: &subject})
	if err != nil || got.SubjectKind != domain.SubjectTicket || *got.SubjectID != subject {
		t.Fatalf("the record's conversation: %+v %v", got, err)
	}
	if _, err = s.CreateConversation(context.Background(), caller, service.NewConversation{SubjectKind: domain.SubjectAccount}); !errors.Is(err, domain.ErrInvalidSubject) {
		t.Errorf("a kind alone: %v", err)
	}
	if list, listErr := s.Conversations(context.Background(), caller, domain.SubjectAccount, nil); listErr != nil || list == nil ||
		query != "subject_kind=account" {
		t.Errorf("by kind: %v %v %q", list, listErr, query)
	}
	if _, err = s.Conversations(context.Background(), caller, "", &subject); !errors.Is(err, domain.ErrInvalidSubject) {
		t.Errorf("an id alone: %v", err)
	}
}
