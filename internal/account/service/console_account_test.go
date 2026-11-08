package service_test

import (
	"context"
	"testing"

	"github.com/shopnexus/server2/internal/platform/page"
)

func TestConsoleAccounts(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if a, err := at(s, nil, now).ConsoleAccount(context.Background(), id); err != nil || a.ID != id || a.DisplayName != "Linh" {
		t.Fatalf("one: %+v %v", a, err)
	}
	newest := seed(t, s)
	first, err := at(s, nil, now).ConsoleAccounts(context.Background(), page.Request{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != newest || first.NextCursor == nil || *first.NextCursor != newest {
		t.Fatalf("first page: %+v %v", first, err)
	}
	rest, err := at(s, nil, now).ConsoleAccounts(context.Background(), page.Request{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(rest.Items) != 1 || rest.Items[0].ID != id || rest.NextCursor != nil {
		t.Fatalf("second page: %+v %v", rest, err)
	}
}
