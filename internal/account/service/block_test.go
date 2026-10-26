package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

func TestBlock(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	svc := at(s, now)
	a, b := seed(t, s), seed(t, s)
	if err := svc.Block(ctx, a, a); !errors.Is(err, domain.ErrSelfBlock) {
		t.Fatalf("self: %v", err)
	}
	if err := svc.Block(ctx, a, uuid.NewV7()); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if err := svc.Block(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	if blocked, err := svc.Blocked(ctx, b, a); err != nil || !blocked {
		t.Fatalf("blocked: %v %v", blocked, err)
	}
	p, err := svc.Blocks(ctx, a, page.Request{})
	if err != nil || len(p.Items) != 1 || p.Items[0].AccountID != b || p.Items[0].DisplayName != "Linh" || p.Items[0].At.IsZero() {
		t.Fatalf("%+v %v", p, err)
	}
	if err = svc.Unblock(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := svc.Blocked(ctx, a, b); blocked {
		t.Fatal("still blocked")
	}
}
