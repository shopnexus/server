package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/agent/service"
)

// An agent's row is saved before Bifrost: a create Bifrost fails is kept,
// and the same create again reconciles it; a refused change leaves the
// row and Bifrost as they were.
func TestAgentRowBeforeBifrost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, db, admin := settingsOn(t)
	in := service.NewAgent{Name: "Writer", Slug: "writer", Model: haiku, MonthlyBudgetUSD: new(4.0)}
	f.Fail("POST /api/governance/virtual-keys")
	if _, err := s.CreateAgent(ctx, admin, in); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Fatalf("a create Bifrost fails: %v", err)
	}
	f.Fail()
	if _, ok := f.Keys()["writer"]; ok {
		t.Fatal("the key after a failed write")
	}
	created, err := s.CreateAgent(ctx, admin, in)
	if err != nil {
		t.Fatalf("the create again: %v", err)
	}
	if k, ok := f.Keys()["writer"]; !ok || k.Metadata["model"] != "upstream/claude-haiku-5-5" {
		t.Errorf("the key after the retry %+v", k)
	}
	if tm := f.Teams()["writer"]; len(tm.Budgets) != 1 || tm.Budgets[0].MaxLimit != 4 {
		t.Errorf("the team after the retry %+v", tm)
	}
	if again, againErr := s.CreateAgent(ctx, admin, in); againErr != nil || again.ID != created.ID {
		t.Errorf("a third create %+v %v", again, againErr)
	}
	if _, err = s.CreateAgent(ctx, admin, service.NewAgent{Name: "Writer", Slug: "writer", Model: sonnet}); !errors.Is(err, domain.ErrAgentTaken) {
		t.Errorf("another agent with the name: %v", err)
	}
	// A slug that is another agent's caller is taken.
	if _, err = s.CreateAgent(ctx, admin, service.NewAgent{Name: "Trust", Slug: "server-trust", Model: haiku}); !errors.Is(err, domain.ErrAgentTaken) {
		t.Errorf("a slug that is a caller: %v", err)
	}

	// Another agent's name refuses the change before Bifrost hears of it.
	if _, err = s.UpdateAgent(ctx, admin, created.ID, service.AgentPatch{Name: new("Console assistant"), Model: &sonnet}); !errors.Is(err, domain.ErrAgentTaken) {
		t.Fatalf("a taken name: %v", err)
	}
	if k := f.Keys()["writer"]; k.Metadata["model"] != "upstream/claude-haiku-5-5" {
		t.Errorf("the key after a refused change %+v", k)
	}
	// A change Bifrost fails is saved, and the next reconcile applies it.
	f.Fail("PUT /api/governance/virtual-keys/" + f.Keys()["writer"].ID)
	if _, err = s.UpdateAgent(ctx, admin, created.ID, service.AgentPatch{Model: &sonnet}); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Fatalf("a change Bifrost fails: %v", err)
	}
	f.Fail()
	a, err := repository.NewSettings(db).Agent(ctx, created.ID)
	if err != nil || a.Model != sonnet {
		t.Fatalf("the saved agent %+v %v", a, err)
	}
	if err = s.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if k := f.Keys()["writer"]; k.Metadata["model"] != "upstream/claude-sonnet-5-5" {
		t.Errorf("the key after the reconcile %+v", k)
	}
}

// Setting one model's tags at once from many requests leaves one request's
// tags, never their union.
func TestModelTagsTakeTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _, db, admin := settingsOn(t)
	gpt := domain.ModelRef{Provider: "upstream", Model: "gpt-5"}
	sets := [][]string{{"Chat"}, {"Vision"}, {"Reasoning"}, {"Multilingual"}, {"Tool use"}, {"Long context"}}
	for range 10 {
		var wg sync.WaitGroup
		for _, tags := range sets {
			wg.Go(func() {
				if _, err := s.SetModelTags(ctx, admin, service.ModelTags{Provider: gpt.Provider, Model: gpt.Model, Tags: tags}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got, err := repository.NewSettings(db).ModelTags(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got[gpt]) != 1 {
			t.Fatalf("gpt-5's tags %v", got[gpt])
		}
	}
}

// A reconcile goes on past an object Bifrost fails: the other callers'
// keys are still written, and the failure comes back as unavailable.
func TestReconcileGoesOnPastAFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, f, _, admin := settingsOn(t)
	created, err := s.CreateAgent(ctx, admin, service.NewAgent{Name: "Writer", Slug: "writer", Model: haiku})
	if err != nil {
		t.Fatal(err)
	}
	f.Fail("PUT /api/governance/virtual-keys/" + f.Keys()["writer"].ID)
	if _, err = s.UpdateAgent(ctx, admin, created.ID, service.AgentPatch{Model: &sonnet}); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Fatalf("a change Bifrost fails: %v", err)
	}
	if err = f.Admin().DeleteVirtualKey(ctx, f.Keys()["server-trust"].ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Reconcile(ctx); !errors.Is(err, domain.ErrGatewayUnavailable) {
		t.Fatalf("a reconcile with a failing key: %v", err)
	}
	if _, ok := f.Keys()["server-trust"]; !ok {
		t.Error("the reconcile stopped before server-trust's key")
	}
	f.Fail()
}
