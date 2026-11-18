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

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

var caller = authn.ConsoleMember{MemberID: uuid.NewV7(), Role: "moderator"}

// agentAt builds the service on an agent served by h, with the AI
// settings on a migrated database.
func agentAt(t *testing.T, h http.HandlerFunc) *service.Agent {
	t.Helper()
	return agentOn(t, repository.NewSettings(pgtest.New(t)), h)
}

// agentOn builds the service on an agent served by h and the settings st.
func agentOn(t *testing.T, st repository.Settings, h http.HandlerFunc) *service.Agent {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return service.New(service.Deps{Settings: st, BaseURL: srv.URL, Token: "service-token", HTTP: &http.Client{}, Now: time.Now})
}

// The question goes to the agent with the service token, the member's id
// and role, and the Console assistant's configuration; the agent answers
// with the turn at once. UC-MOD-08.
func TestAskForwardsTheMemberAndTheAgent(t *testing.T) {
	t.Parallel()
	conversation, turn, skill := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	ticket := uuid.NewV7()
	s := agentAt(t, func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text        string               `json:"text"`
			SkillID     uuid.UUID            `json:"skill_id"`
			Attachments []service.Attachment `json:"attachments"`
			AgentConfig service.AgentConfig  `json:"agent_config"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		cfg := in.AgentConfig
		if r.Method != http.MethodPost || r.URL.Path != "/conversations/"+conversation.String()+"/turns" || in.Text != "Ai mở ticket?" ||
			r.Header.Get("Authorization") != "Bearer service-token" || r.Header.Get("X-Console-Member") != caller.MemberID.String() ||
			r.Header.Get("X-Console-Role") != "moderator" || in.SkillID != skill || len(in.Attachments) != 1 ||
			in.Attachments[0].ID != ticket || cfg.ID != domain.ConsoleAssistant || cfg.Alias != "console-agent" ||
			cfg.Name != "Console assistant" || cfg.Instructions == "" || cfg.InstructionsVersion != 1 || len(cfg.Tools) != 16 ||
			cfg.Limits.DeadlineS != 60 {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"turn_id": turn})
	})
	got, err := s.Ask(context.Background(), caller, conversation, service.NewTurn{
		Text: "Ai mở ticket?", SkillID: &skill, Attachments: []service.Attachment{{Kind: "ticket", ID: ticket}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ConversationID != conversation || got.TurnID != turn {
		t.Fatalf("%+v", got)
	}
	app := uuid.MustParse("0193e000-0000-7000-8000-00000000a102")
	if _, err = s.Ask(context.Background(), caller, conversation, service.NewTurn{Text: "?", AgentID: &app}); !errors.Is(err, domain.ErrAgentNotFound) {
		t.Errorf("an app agent: %v", err)
	}
	unknown := uuid.NewV7()
	if _, err = s.Ask(context.Background(), caller, conversation, service.NewTurn{Text: "?", AgentID: &unknown}); !errors.Is(err, domain.ErrAgentNotFound) {
		t.Errorf("an unknown agent: %v", err)
	}
}

// Each failure of the agent reaches the member as the code the console
// shows, by the agent's code, else by its status. UC-MOD-08 A1, E1.
func TestAskMapsTheAgentsFailures(t *testing.T) {
	t.Parallel()
	st := repository.NewSettings(pgtest.New(t))
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
		{http.StatusConflict, "conversation_closed", domain.ErrConversationClosed},
		{http.StatusConflict, "turn_state", domain.ErrTurnState},
		{http.StatusForbidden, "forbidden", domain.ErrForbidden},
		{http.StatusUnauthorized, "unauthorized", domain.ErrUnavailable},
		{http.StatusInternalServerError, "internal", domain.ErrUnavailable},
		{http.StatusNotFound, "", domain.ErrConversationNotFound},
		{http.StatusGatewayTimeout, "", domain.ErrTimeout},
		{http.StatusBadGateway, "", domain.ErrUnavailable},
	} {
		s := agentOn(t, st, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			if c.code != "" {
				_ = json.NewEncoder(w).Encode(map[string]string{"code": c.code})
			}
		})
		if _, err := s.Ask(context.Background(), caller, uuid.NewV7(), service.NewTurn{Text: "?"}); !errors.Is(err, c.want) {
			t.Errorf("%d %q: %v, want %v", c.status, c.code, err, c.want)
		}
	}
	s := agentOn(t, st, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) })
	if _, err := s.Ask(context.Background(), caller, uuid.NewV7(), service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("a body that does not read: %v", err)
	}
	down := service.New(service.Deps{
		Settings: st, BaseURL: "http://127.0.0.1:1", Token: "t", HTTP: &http.Client{}, Now: time.Now,
	})
	if _, err := down.Ask(context.Background(), caller, uuid.NewV7(), service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("an agent that does not answer: %v", err)
	}
	stall := make(chan struct{})
	defer close(stall)
	slow := agentOn(t, st, func(http.ResponseWriter, *http.Request) { <-stall })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := slow.Ask(ctx, caller, uuid.NewV7(), service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("an agent that does not take the turn: %v", err)
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
