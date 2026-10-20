package auth_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/session/sessiontest"
)

func TestListSessionsPagesNewestFirst(t *testing.T) {
	s, acc := sessiontest.NewMemStore(), uuid.NewV7()
	ps := devices(t, s, acc, 2, now)
	devices(t, s, uuid.NewV7(), 1, now)
	ctx := context.Background()

	all, err := auth.ListSessions(ctx, s, ps[1], uuid.UUID{}, 0, now)
	if err != nil || len(all.Items) != 2 || all.NextCursor != nil || all.Items[0].ID != ps[1].SessionID ||
		!all.Items[0].Current || all.Items[1].Current {
		t.Fatalf("%+v %v", all, err)
	}
	first, _ := auth.ListSessions(ctx, s, ps[1], uuid.UUID{}, 1, now)
	if len(first.Items) != 1 || first.NextCursor == nil || *first.NextCursor != ps[1].SessionID {
		t.Fatalf("first page %+v", first)
	}
	second, _ := auth.ListSessions(ctx, s, ps[1], *first.NextCursor, 1, now)
	if len(second.Items) != 1 || second.Items[0].ID != ps[0].SessionID || second.NextCursor != nil {
		t.Fatalf("second page %+v", second)
	}
}
