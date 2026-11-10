package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

func TestSearchRemembersTheFirstPage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	bike := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.model.answers["xe đạp"] = `{"understood":"Xe đạp","boosts":[{"attr":"category","values":["vehicles.bicycle"]}],"demotes":[]}`

	if _, err := e.discovery.Search(ctx, &me, service.SearchInput{Query: " xe đạp "}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []service.SearchInput{{Query: "tủ lạnh", Cursor: "20"}, {Like: bike}, {}} {
		if _, err := e.discovery.Search(ctx, &me, in); err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
	}
	if _, err := e.discovery.Search(ctx, nil, service.SearchInput{Query: "máy giặt"}); err != nil {
		t.Fatal(err)
	}
	h, err := e.discovery.History(ctx, me)
	if err != nil || len(h.Items) != 1 {
		t.Fatalf("history: %+v %v", h, err)
	}
	if got := h.Items[0]; got.Query != "xe đạp" || got.Title != "Xe đạp" || got.Category == "" || got.PhotoURL == "" {
		t.Fatalf("entry: %+v", got)
	}
	if err = e.discovery.ForgetSearch(ctx, me, h.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if h, _ = e.discovery.History(ctx, me); len(h.Items) != 0 {
		t.Fatalf("after forget: %+v", h)
	}
	if _, err = e.discovery.Search(ctx, &me, service.SearchInput{Query: "tủ lạnh"}); err != nil {
		t.Fatal(err)
	}
	if err = e.discovery.ClearHistory(ctx, me); err != nil {
		t.Fatal(err)
	}
	if h, _ = e.discovery.History(ctx, me); len(h.Items) != 0 {
		t.Fatalf("after clear: %+v", h)
	}
}

// failingHistory is a history store whose Remember fails.
type failingHistory struct{ repository.History }

func (failingHistory) Remember(context.Context, uuid.UUID, repository.HistoryEntry) error {
	return errors.New("db down")
}

func TestSearchSurvivesAFailedRemember(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(d *service.Deps) { d.History = failingHistory{d.History} })
	me := accounttest.New(t, e.db)
	if _, err := e.discovery.Search(context.Background(), &me, service.SearchInput{Query: "xe đạp"}); err != nil {
		t.Fatalf("the search failed with the history: %v", err)
	}
}

func TestAccountDeletedClearsSavesAndHistory(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	if err := e.discovery.Save(ctx, me, e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.discovery.Search(ctx, &me, service.SearchInput{Query: "xe đạp"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := e.discovery.AccountDeleted(ctx, me); err != nil {
			t.Fatal(err)
		}
	}
	if h, _ := e.discovery.History(ctx, me); len(h.Items) != 0 {
		t.Fatalf("history left: %+v", h)
	}
	if saved, _ := e.discovery.Saved(ctx, me, page.Request{}); len(saved.Items) != 0 {
		t.Fatalf("saves left: %+v", saved)
	}
}
