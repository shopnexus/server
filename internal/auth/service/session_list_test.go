package service_test

import (
	"context"
	"testing"

	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func TestListSessionsPagesNewestFirst(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSessions(db)
	ps := devices(t, s, newAccount(t, db), 2, now)
	devices(t, s, newAccount(t, db), 1, now)
	ctx := context.Background()

	all, err := authWith(s, nil, now, discard).ListSessions(ctx, ps[1], page.Request{})
	if err != nil || len(all.Items) != 2 || all.NextCursor != nil || all.Items[0].ID != ps[1].SessionID ||
		!all.Items[0].Current || all.Items[1].Current {
		t.Fatalf("%+v %v", all, err)
	}
	first, _ := authWith(s, nil, now, discard).ListSessions(ctx, ps[1], page.Request{Limit: 1})
	if len(first.Items) != 1 || first.NextCursor == nil || *first.NextCursor != ps[1].SessionID {
		t.Fatalf("first page %+v", first)
	}
	second, _ := authWith(s, nil, now, discard).ListSessions(ctx, ps[1], page.Request{Cursor: *first.NextCursor, Limit: 1})
	if len(second.Items) != 1 || second.Items[0].ID != ps[0].SessionID || second.NextCursor != nil {
		t.Fatalf("second page %+v", second)
	}
}
