package service_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/audit"
)

// An agent's row is saved before LiteLLM: a refused save leaves LiteLLM as
// it was, and the same create or change again writes LiteLLM from the row.
func TestAgentRowBeforeLiteLLM(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, _, admin := settingsOn(t)
	if _, err := s.CreateProvider(ctx, admin, service.NewProvider{ID: "relay", Kind: "openai", APIKey: "sk-relay"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"claude-haiku-5-5", "claude-sonnet-5-5"} {
		if _, err := s.AddProviderModel(ctx, admin, "relay", m, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	in := service.NewAgent{Name: "Writer", Alias: "writer", Model: "claude-haiku-5-5", MonthlyBudgetUSD: new(4.0)}
	f.failing("POST /model/new")
	if _, err := s.CreateAgent(ctx, admin, in); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Fatalf("a create LiteLLM fails: %v", err)
	}
	f.failing()
	if got := f.aliases("writer"); len(got) != 0 {
		t.Fatalf("the alias after a failed write %v", got)
	}
	created, err := s.CreateAgent(ctx, admin, in)
	if err != nil {
		t.Fatalf("the create again: %v", err)
	}
	if got := f.aliases("writer"); !slices.Equal(got, []string{"relay:openai/claude-haiku-5-5:1:1"}) {
		t.Errorf("the alias after the retry %v", got)
	}
	k := f.key("agent")
	if models, ok := k["models"].([]any); !ok || !slices.Contains(models, any("writer")) {
		t.Errorf("the agent key's models %v", k["models"])
	}
	if per, ok := k["model_max_budget"].(map[string]any); !ok || per["writer"] == nil {
		t.Errorf("the alias's budget %v", k["model_max_budget"])
	}
	again, err := s.CreateAgent(ctx, admin, in)
	if err != nil || again.ID != created.ID {
		t.Errorf("a third create %+v %v", again, err)
	}
	if _, err = s.CreateAgent(ctx, admin, service.NewAgent{Name: "Writer", Alias: "writer", Model: "claude-sonnet-5-5"}); !errors.Is(err, domain.ErrAgentTaken) {
		t.Errorf("another agent with the name: %v", err)
	}

	// Another agent's name refuses the change before the alias moves.
	if _, err = s.UpdateAgent(ctx, admin, created.ID, service.AgentPatch{
		Name: new("Console assistant"), Model: new("claude-sonnet-5-5"),
	}); !errors.Is(err, domain.ErrAgentTaken) {
		t.Fatalf("a taken name: %v", err)
	}
	if got := f.aliases("writer"); !slices.Equal(got, []string{"relay:openai/claude-haiku-5-5:1:1"}) {
		t.Errorf("the alias after a refused change %v", got)
	}

	// A change LiteLLM fails is saved, and the same change again applies it.
	patch := service.AgentPatch{Model: new("claude-sonnet-5-5"), MonthlyBudgetUSD: service.Budget{Set: true, Value: new(7.0)}}
	f.failing("POST /key/update")
	if _, err = s.UpdateAgent(ctx, admin, created.ID, patch); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Fatalf("a change LiteLLM fails: %v", err)
	}
	f.failing()
	got, err := s.UpdateAgent(ctx, admin, created.ID, patch)
	if err != nil || got.Model != "claude-sonnet-5-5" || *got.MonthlyBudgetUSD != 7 {
		t.Fatalf("the change again %+v %v", got, err)
	}
	if aliases := f.aliases("writer"); !slices.Equal(aliases, []string{"relay:openai/claude-sonnet-5-5:1:1"}) {
		t.Errorf("the alias after the change %v", aliases)
	}
	per, ok := f.key("agent")["model_max_budget"].(map[string]any)
	if b, bOK := per["writer"].(map[string]any); !ok || !bOK || b["max_budget"] != 7.0 {
		t.Errorf("the alias's budget %v", per)
	}
}

// Setting one model's tags at once from many requests leaves one request's
// tags, never their union.
func TestModelTagsTakeTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _, db, admin := settingsOn(t)
	sets := [][]string{{"Chat"}, {"Vision"}, {"Reasoning"}, {"Multilingual"}, {"Tool use"}, {"Long context"}}
	for range 10 {
		var wg sync.WaitGroup
		for _, tags := range sets {
			wg.Go(func() {
				if _, err := s.SetModelTags(ctx, admin, "gpt-5", tags); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got, err := repository.NewSettings(db).ModelTags(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got["gpt-5"]) != 1 {
			t.Fatalf("gpt-5's tags %v", got["gpt-5"])
		}
	}
}

// The budget settings are saved before LiteLLM: an unknown member writes
// nothing to LiteLLM, a stale write conflicts, and the same change again
// after LiteLLM failed applies it.
func TestBudgetsRowBeforeLiteLLM(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, db, admin := settingsOn(t)
	daily := &service.RoleBudgetsPatch{Moderator: service.Budget{Set: true, Value: new(2.0)}}
	if _, err := s.UpdateBudgets(ctx, admin, service.BudgetsPatch{
		MemberDailyUSD: daily, Overrides: &[]service.MemberDailyUSD{{MemberID: uuid.NewV7(), DailyUSD: 1}},
	}); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("an unknown member: %v", err)
	}
	f.mu.Lock()
	budgets := len(f.budget)
	f.mu.Unlock()
	if budgets != 0 {
		t.Errorf("LiteLLM took %d budgets of a refused save", budgets)
	}

	f.failing("POST /budget/new")
	if _, err := s.UpdateBudgets(ctx, admin, service.BudgetsPatch{MemberDailyUSD: daily}); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Fatalf("a change LiteLLM fails: %v", err)
	}
	f.failing()
	settings := repository.NewSettings(db)
	set, err := settings.BudgetSettings(ctx)
	if err != nil || set.ModeratorDailyUSD == nil || *set.ModeratorDailyUSD != 2 {
		t.Fatalf("the saved settings %+v %v", set, err)
	}
	if _, err = s.UpdateBudgets(ctx, admin, service.BudgetsPatch{MemberDailyUSD: daily}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	moderators := f.budget["console-member-moderator"]
	f.mu.Unlock()
	if moderators["max_budget"] != 2.0 {
		t.Errorf("the moderators' budget %v", moderators)
	}

	// A save over a row read before another save conflicts.
	stale := set
	stale.AlertPct, stale.WriteID = 50, uuid.NewV7()
	if err = settings.SaveBudgetSettings(ctx, stale, set.WriteID, audit.Entry{
		Actor: audit.ByMember(admin), Action: audit.AIBudgets, Target: audit.AISettings("budgets"), At: settingsNow,
	}); !errors.Is(err, repository.ErrConflict) {
		t.Errorf("a stale save: %v", err)
	}
	if _, err = s.UpdateBudgets(ctx, admin, service.BudgetsPatch{AlertPct: new(60)}); err != nil {
		t.Errorf("a fresh change: %v", err)
	}
}
