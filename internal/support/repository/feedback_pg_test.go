package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
)

func TestAdd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewFeedback(db)
	account := accounttest.New(t, db)
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	since := at.Add(-24 * time.Hour)
	row := func(body string, created time.Time) repository.Row {
		return repository.Row{ID: uuid.NewV7(), AccountID: account, Body: body, CreatedAt: created}
	}

	first := uuid.NewV7()
	r, err := s.Add(ctx, row("một", at), first, since)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Add(ctx, row("khác", at), first, since)
	if err != nil || again.ID != r.ID || again.Body != "một" {
		t.Fatalf("repeated key: %+v %v", again, err)
	}
	for i := range domain.DailyLimit - 1 {
		if _, err = s.Add(ctx, row("x", at.Add(time.Duration(i)*time.Minute)), uuid.NewV7(), since); err != nil {
			t.Fatalf("feedback %d: %v", i+2, err)
		}
	}
	if _, err = s.Add(ctx, row("x", at), uuid.NewV7(), since); !errors.Is(err, domain.ErrFeedbackLimit) {
		t.Fatalf("eleventh: %v", err)
	}
	if again, err = s.Add(ctx, row("x", at), first, since); err != nil || again.ID != r.ID {
		t.Fatalf("repeated key at the limit: %v", err)
	}
	if _, err = s.Add(ctx, row("x", at.Add(25*time.Hour)), uuid.NewV7(), at.Add(time.Hour)); err != nil {
		t.Fatalf("a day later: %v", err)
	}
	rows, err := s.List(ctx, page.Request{Limit: 5})
	if err != nil || len(rows) != 6 || !rows[0].CreatedAt.Equal(at.Add(25*time.Hour)) {
		t.Fatalf("newest first, one extra row: %d %v", len(rows), err)
	}
}
