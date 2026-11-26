package service_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// An account that saved none reads the defaults in its language; a put
// trims each answer and keeps the order; the account's deletion removes
// them.
//
// UC-DEAL-01 A5.
func TestPutQuickReplies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := service.New(service.Deps{Chats: repository.NewChats(db), QuickReplies: repository.NewQuickReplies(db), Now: time.Now})
	me := accounttest.New(t, db)
	if got, err := s.QuickReplies(ctx, me, locale.English); err != nil || !slices.Equal(got.Items, domain.DefaultQuickReplies(locale.English)) {
		t.Fatalf("defaults: %q %v", got.Items, err)
	}
	want := []string{"Còn bạn ơi", "Mình ở Q.1, xem hàng buổi tối được"}
	put, err := s.PutQuickReplies(ctx, me, []string{"  Còn bạn ơi ", "Mình ở Q.1, xem hàng buổi tối được\n"})
	if err != nil || !slices.Equal(put.Items, want) {
		t.Fatalf("put: %q %v", put.Items, err)
	}
	if got, _ := s.QuickReplies(ctx, me, locale.English); !slices.Equal(got.Items, want) {
		t.Fatalf("read: %q", got.Items)
	}
	if err = s.DeleteAccount(ctx, me); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.QuickReplies(ctx, me, locale.English); slices.Equal(got.Items, want) {
		t.Fatalf("kept after deletion: %q", got.Items)
	}
}
