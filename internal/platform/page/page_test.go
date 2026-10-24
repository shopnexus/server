package page_test

import (
	"slices"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
)

func TestBeforeStartsAboveEveryID(t *testing.T) {
	t.Parallel()
	if got := (page.Request{}).Before(); got != uuid.Max() {
		t.Fatalf("first page lists below %s, want the max UUID", got)
	}
	c := uuid.NewV7()
	if got := (page.Request{Cursor: c}).Before(); got != c {
		t.Fatalf("got %s, want the cursor %s", got, c)
	}
}

func TestFetchReadsOneMore(t *testing.T) {
	t.Parallel()
	if got := (page.Request{}).Fetch(); got != page.DefaultLimit+1 {
		t.Fatalf("no limit: fetch %d, want %d", got, page.DefaultLimit+1)
	}
	if got := (page.Request{Limit: 5}).Fetch(); got != 6 {
		t.Fatalf("limit 5: fetch %d, want 6", got)
	}
}

func TestOf(t *testing.T) {
	t.Parallel()
	ids := []uuid.UUID{uuid.NewV7(), uuid.NewV7(), uuid.NewV7()}
	id := func(u uuid.UUID) uuid.UUID { return u }
	r := page.Request{Limit: 2}
	p := page.Of(ids, r, id)
	if !slices.Equal(p.Items, ids[:2]) || p.NextCursor == nil || *p.NextCursor != ids[1] {
		t.Fatalf("a full page and one more: %+v", p)
	}
	p = page.Of(ids[:2], r, id)
	if !slices.Equal(p.Items, ids[:2]) || p.NextCursor != nil {
		t.Fatalf("exactly a page: %+v", p)
	}
	p = page.Of(nil, r, id)
	if p.Items == nil || len(p.Items) != 0 || p.NextCursor != nil {
		t.Fatalf("no rows: %+v", p)
	}
}
