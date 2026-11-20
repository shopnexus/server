package service_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// A put trims each answer and keeps the order; the account's deletion
// removes them.
func TestPutQuickReplies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := service.New(service.Deps{QuickReplies: repository.NewQuickReplies(db), Now: time.Now})
	me := accounttest.New(t, db)
	want := []string{"Còn bạn ơi", "Mình ở Q.1, xem hàng buổi tối được"}
	put, err := s.PutQuickReplies(ctx, me, []string{"  Còn bạn ơi ", "Mình ở Q.1, xem hàng buổi tối được\n"})
	if err != nil || !slices.Equal(put.Items, want) {
		t.Fatalf("put: %q %v", put.Items, err)
	}
	if got, _ := s.QuickReplies(ctx, me); !slices.Equal(got.Items, want) {
		t.Fatalf("read: %q", got.Items)
	}
	if err = s.DeleteAccount(ctx, me); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.QuickReplies(ctx, me); slices.Equal(got.Items, want) {
		t.Fatalf("kept after deletion: %q", got.Items)
	}
}
