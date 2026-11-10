package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
	"github.com/shopnexus/server2/internal/support/service"
)

func TestSend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	accounts := accounttest.Service(db)
	svc := service.New(service.Deps{Feedback: repository.NewFeedback(db), Accounts: accounts, Now: time.Now})
	me := accounttest.New(t, db)

	key := uuid.NewV7()
	v, err := svc.Send(ctx, me, key, "  Ứng dụng chạy chậm ")
	if err != nil || v.Body != "Ứng dụng chạy chậm" {
		t.Fatalf("send: %+v %v", v, err)
	}
	if again, _ := svc.Send(ctx, me, key, "khác"); again.ID != v.ID {
		t.Fatalf("repeated key: %+v", again)
	}
	if _, err = svc.Send(ctx, me, uuid.NewV7(), " "); !errors.Is(err, domain.ErrInvalidFeedback) {
		t.Fatalf("blank: %v", err)
	}
	list, err := svc.Feedback(ctx, page.Request{})
	if err != nil || len(list.Items) != 1 || list.Items[0].Account.ID != me || list.Items[0].Account.DisplayName != "Linh" ||
		list.Items[0].Account.Deleted {
		t.Fatalf("list: %+v %v", list, err)
	}

	suspended := accounttest.New(t, db)
	accounttest.Suspend(t, accounts, suspended)
	if _, err = svc.Send(ctx, suspended, uuid.NewV7(), "x"); !errors.Is(err, accountdomain.ErrSuspended) {
		t.Fatalf("suspended: %v", err)
	}
	if err = accounts.Erase(ctx, me); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Send(ctx, me, uuid.NewV7(), "x"); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("deleted: %v", err)
	}
	if list, _ = svc.Feedback(ctx, page.Request{}); !list.Items[0].Account.Deleted || list.Items[0].Account.DisplayName != "" {
		t.Fatalf("a deleted author: %+v", list.Items[0].Account)
	}
}
