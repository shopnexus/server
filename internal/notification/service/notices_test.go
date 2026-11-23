package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/notification/repository"
	"github.com/shopnexus/server/internal/notification/service"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestNotices(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	store := repository.NewNotices(db)
	s := service.New(service.Deps{Notices: store, Now: time.Now})
	me, stranger := accounttest.New(t, db), accounttest.New(t, db)
	deal := uuid.NewV7()
	id, _, err := store.Add(ctx, uuid.NewV7(), domain.Notice{
		Recipient: me, Type: domain.TypeOffer, CollapseKey: "deal:" + deal.String(), At: time.Now(),
		Params: domain.Params{DealID: deal, Actor: "Đức Huy", Amount: 1_200_000, Title: "Xe đạp Asama"},
	}, time.Now().Add(-domain.Retention))
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.List(ctx, me, page.Request{}, "")
	if err != nil || len(l.Items) != 1 || l.Unread != 1 || l.Items[0].Text != "Đức Huy trả 1,2 tr cho Xe đạp Asama" ||
		l.Items[0].Target.Kind != "deal" || l.Items[0].Target.ID != deal.String() || !l.Items[0].Unread {
		t.Fatalf("%+v %v", l, err)
	}
	if err = s.Read(ctx, stranger, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if err = s.Read(ctx, me, id); err != nil {
		t.Fatal(err)
	}
	session := uuid.NewV7()
	for _, bad := range []string{"", " ", strings.Repeat("x", domain.MaxToken+1)} {
		if err = s.PutToken(ctx, session, me, bad); !errors.Is(err, domain.ErrInvalidToken) {
			t.Fatalf("token %q: %v", bad[:min(len(bad), 5)], err)
		}
	}
	if err = s.PutToken(ctx, session, me, "fcm-token"); err != nil {
		t.Fatal(err)
	}
	if tokens, _ := store.Tokens(ctx, me); len(tokens) != 1 || tokens[0].Session != session {
		t.Fatalf("%+v", tokens)
	}
	if err = s.DeleteToken(ctx, session); err != nil {
		t.Fatal(err)
	}
	if tokens, _ := store.Tokens(ctx, me); len(tokens) != 0 {
		t.Fatalf("after delete: %+v", tokens)
	}
}

// A notice stored before params were typed, with every key of its deal, a
// carrier label and numbers as JSON writes them, still reads. It names no
// order, so it opens its deal.
func TestStoredParams(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := service.New(service.Deps{Notices: repository.NewNotices(db), Now: time.Now})
	me, deal := accounttest.New(t, db), uuid.NewV7()
	if _, err := db.Exec(ctx, `INSERT INTO notifications (id, account_id, notice_type, collapse_key, params, event_id, created_at, bumped_at)
VALUES (@id, @account, 'shipped', @key, @params, @id, now(), now())`, pgx.StrictNamedArgs{
		"id": uuid.NewV7(), "account": me, "key": "deal:" + deal.String(),
		"params": `{"deal_id": "` + deal.String() + `", "title": "Xe đạp Asama", "actor": "Hoàng Nam", "seller": "Hoàng Nam",
"amount": 0, "move": "shipped", "by": "seller", "carrier": "GHN", "code": "8A2K9", "reason": ""}`,
	}); err != nil {
		t.Fatal(err)
	}
	l, err := s.List(ctx, me, page.Request{}, "")
	if err != nil || len(l.Items) != 1 || l.Items[0].Text != "Hoàng Nam đã gửi Xe đạp Asama · GHN 8A2K9" ||
		l.Items[0].Target.Kind != "deal" || l.Items[0].Target.ID != deal.String() {
		t.Fatalf("%+v %v", l, err)
	}
}
