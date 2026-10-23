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

func TestCut(t *testing.T) {
	t.Parallel()
	r := page.Request{Limit: 2}
	kept, more := page.Cut([]int{1, 2, 3}, r)
	if !slices.Equal(kept, []int{1, 2}) || !more {
		t.Fatalf("a full page and one more: got %v %v", kept, more)
	}
	kept, more = page.Cut([]int{1, 2}, r)
	if !slices.Equal(kept, []int{1, 2}) || more {
		t.Fatalf("exactly a page: got %v %v", kept, more)
	}
	kept, more = page.Cut([]int{}, r)
	if len(kept) != 0 || more {
		t.Fatalf("no rows: got %v %v", kept, more)
	}
}
