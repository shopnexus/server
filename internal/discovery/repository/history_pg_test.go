package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func TestHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	h := repository.NewHistory(db)
	account, other := accounttest.New(t, db), accounttest.New(t, db)

	for i := range 12 {
		if err := h.Remember(ctx, account, repository.HistoryEntry{
			Query: fmt.Sprintf("tìm %d", i), SearchedAt: now.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Remember(ctx, other, repository.HistoryEntry{Query: "tìm 0", SearchedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := h.Remember(ctx, account, repository.HistoryEntry{
		Query: "TÌM  5", Category: "Xe đạp", PhotoURL: "https://cdn.example/x.jpg", SearchedAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	list, err := h.List(ctx, account)
	if err != nil || len(list) != domain.MaxHistory {
		t.Fatalf("list: %d %v", len(list), err)
	}
	if list[0].Query != "TÌM  5" || list[0].Category != "Xe đạp" || list[0].PhotoURL == "" {
		t.Fatalf("top: %+v", list[0])
	}
	kept := map[string]bool{}
	for _, e := range list {
		kept[e.Query] = true
	}
	if kept["tìm 0"] || kept["tìm 1"] || kept["tìm 5"] || !kept["tìm 2"] || !kept["tìm 11"] {
		t.Fatalf("kept: %v", kept)
	}
	if err = h.Forget(ctx, account, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = h.Forget(ctx, other, list[1].ID); err != nil {
		t.Fatalf("forget someone else's entry: %v", err)
	}
	if err = h.Forget(ctx, account, uuid.NewV7()); err != nil {
		t.Fatalf("forget an unknown entry: %v", err)
	}
	if list, _ = h.List(ctx, account); len(list) != domain.MaxHistory-1 {
		t.Fatalf("after forget: %d", len(list))
	}
	if err = h.Clear(ctx, account); err != nil {
		t.Fatal(err)
	}
	if list, _ = h.List(ctx, account); len(list) != 0 {
		t.Fatalf("after clear: %d", len(list))
	}
	if list, _ = h.List(ctx, other); len(list) != 1 {
		t.Fatalf("the other account's history: %d", len(list))
	}
}
