package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func TestNotices(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	store := repository.NewNotices(db)
	s := service.New(service.Deps{Notices: store, Now: time.Now})
	me, stranger := uuid.NewV7(), uuid.NewV7()
	for _, id := range []uuid.UUID{me, stranger} {
		if _, err := db.Exec(ctx, `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
	deal := uuid.NewV7()
	id, _, err := store.Add(ctx, uuid.NewV7(), domain.Notice{
		Recipient: me, Type: domain.TypeOffer, CollapseKey: "deal:" + deal.String(), At: time.Now(),
		Params: map[string]any{"deal_id": deal.String(), "actor": "Đức Huy", "amount": 1_200_000, "title": "Xe đạp Asama"},
	}, time.Now().Add(-domain.Retention))
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.List(ctx, me, page.Request{})
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
	p := authn.Principal{AccountID: me, SessionID: uuid.NewV7()}
	for _, bad := range []string{"", " ", strings.Repeat("x", domain.MaxToken+1)} {
		if err = s.PutToken(ctx, p, bad); !errors.Is(err, domain.ErrInvalidToken) {
			t.Fatalf("token %q: %v", bad[:min(len(bad), 5)], err)
		}
	}
	if err = s.PutToken(ctx, p, "fcm-token"); err != nil {
		t.Fatal(err)
	}
	if tokens, _ := store.Tokens(ctx, me); len(tokens) != 1 || tokens[0].Session != p.SessionID {
		t.Fatalf("%+v", tokens)
	}
	if err = s.DeleteToken(ctx, p); err != nil {
		t.Fatal(err)
	}
}
