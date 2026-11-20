package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatrepo "github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// conversation opens the conversation of two new accounts.
func conversation(t *testing.T, db *pgxpool.Pool) (c chatdomain.Conversation) {
	t.Helper()
	c, err := chatrepo.NewChats(db).OpenPair(context.Background(),
		chatdomain.NewConversation(uuid.NewV7(), accounttest.New(t, db), accounttest.New(t, db), time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func check(c chatdomain.Conversation, at time.Time, a []domain.Code) domain.Check {
	return domain.Check{
		ID: uuid.NewV7(), ConversationID: c.ID, MessageIDs: []uuid.UUID{uuid.NewV7(), uuid.NewV7()},
		Classifier: "test", PromptVersion: 1, A: a, B: []domain.Code{}, CreatedAt: at,
	}
}

func updates(t *testing.T, db *pgxpool.Pool, account uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM account_updates WHERE account_id = @a AND kind = 'scam_warning'`,
		pgx.StrictNamedArgs{"a": account}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestScamWarnings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewScamWarnings(db)
	c := conversation(t, db)
	at := time.Now().UTC().Truncate(time.Microsecond)

	if last, n, err := s.Checked(ctx, c.ID, at.Add(-24*time.Hour)); err != nil || last != nil || n != 0 {
		t.Fatalf("no checks: %v %d %v", last, n, err)
	}
	w := domain.NewWarning(c.ID, c.AccountB, c.AccountA)
	w.Raise([]domain.Code{domain.CodePrepayment}, nil, at, nil)
	ck := check(c, at, []domain.Code{domain.CodePrepayment})
	writeID := uuid.NewV7()
	if err := s.Record(ctx, ck, []repository.WarningWrite{{Warning: &w, WriteID: writeID}}, at.Add(-domain.CheckRetention)); err != nil {
		t.Fatal(err)
	}
	// A retried step finds the warning saved, raises nothing and writes the
	// check again under its ID, which adds no row.
	if err := s.Record(ctx, ck, nil, at.Add(-domain.CheckRetention)); err != nil {
		t.Fatal(err)
	}
	if w.WriteID() != writeID {
		t.Fatalf("write id %v", w.WriteID())
	}
	last, n, err := s.Checked(ctx, c.ID, at.Add(-24*time.Hour))
	if err != nil || !slices.Equal(last, ck.MessageIDs) || n != 1 {
		t.Fatalf("checked: %v %d %v", last, n, err)
	}
	got, err := s.Get(ctx, c.ID, c.AccountB)
	if err != nil || got.SuspectID() != c.AccountA || len(got.Signals()) != 1 || got.Signals()[0].Code != domain.CodePrepayment ||
		!got.Signals()[0].FirstSeenAt.Equal(at) || got.WriteID() != writeID {
		t.Fatalf("get: %+v %v", got.State(), err)
	}
	if updates(t, db, c.AccountB) != 1 || updates(t, db, c.AccountA) != 0 {
		t.Fatal("only the warned member hears of it")
	}
	if _, err = s.Get(ctx, c.ID, c.AccountA); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("the suspect has none: %v", err)
	}
	many, err := s.Of(ctx, c.AccountB, []uuid.UUID{c.ID, uuid.NewV7()})
	if mw := many[c.ID]; err != nil || len(many) != 1 || mw.AccountID() != c.AccountB {
		t.Fatalf("of: %v %v", many, err)
	}

	if _, err = s.Dismiss(ctx, c.ID, c.AccountB, at.Add(time.Minute), uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Get(ctx, c.ID, c.AccountB); !again.Dismissed() || updates(t, db, c.AccountB) != 2 {
		t.Fatalf("dismissed: %+v", again.State())
	}
}

// A write over a stale write ID is ErrConflict and changes nothing.
func TestScamWarningsConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewScamWarnings(db)
	c := conversation(t, db)
	at := time.Now().UTC().Truncate(time.Microsecond)
	w := domain.NewWarning(c.ID, c.AccountB, c.AccountA)
	w.Raise([]domain.Code{domain.CodePrepayment}, nil, at, nil)
	if err := s.Record(ctx, check(c, at, []domain.Code{domain.CodePrepayment}), []repository.WarningWrite{{Warning: &w, WriteID: uuid.NewV7()}},
		at.Add(-domain.CheckRetention)); err != nil {
		t.Fatal(err)
	}
	stale, _ := s.Get(ctx, c.ID, c.AccountB)
	if _, err := s.Dismiss(ctx, c.ID, c.AccountB, at.Add(time.Minute), uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	stale.Raise([]domain.Code{domain.CodeSensitive}, nil, at.Add(2*time.Minute), nil)
	err := s.Record(ctx, check(c, at.Add(2*time.Minute), []domain.Code{domain.CodeSensitive}),
		[]repository.WarningWrite{{Warning: &stale, WriteID: uuid.NewV7()}}, at.Add(-domain.CheckRetention))
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale record: %v", err)
	}
	if _, n, _ := s.Checked(ctx, c.ID, at.Add(-time.Hour)); n != 1 {
		t.Fatalf("the conflict wrote a check: %d", n)
	}
	// A new warning where a row exists is a conflict too.
	dup := domain.NewWarning(c.ID, c.AccountB, c.AccountA)
	dup.Raise([]domain.Code{domain.CodePressure}, nil, at, nil)
	err = s.Record(ctx, check(c, at, []domain.Code{domain.CodePressure}), []repository.WarningWrite{{Warning: &dup, WriteID: uuid.NewV7()}},
		at.Add(-domain.CheckRetention))
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("duplicate insert: %v", err)
	}
}

// Deleting an account deletes its own warnings; those about it stay.
func TestScamWarningsDeleteAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewScamWarnings(db)
	c := conversation(t, db)
	at := time.Now().UTC().Truncate(time.Microsecond)
	toB, toA := domain.NewWarning(c.ID, c.AccountB, c.AccountA), domain.NewWarning(c.ID, c.AccountA, c.AccountB)
	toB.Raise([]domain.Code{domain.CodePrepayment}, nil, at, nil)
	toA.Raise([]domain.Code{domain.CodeFakeProof}, nil, at, nil)
	if err := s.Record(ctx, check(c, at, []domain.Code{domain.CodePrepayment}),
		[]repository.WarningWrite{{Warning: &toB, WriteID: uuid.NewV7()}, {Warning: &toA, WriteID: uuid.NewV7()}},
		at.Add(-domain.CheckRetention)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.DeleteAccount(ctx, c.AccountA); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Get(ctx, c.ID, c.AccountA); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("A's own warning: %v", err)
	}
	if _, err := s.Get(ctx, c.ID, c.AccountB); err != nil {
		t.Fatalf("B's warning about A: %v", err)
	}
}

// Recording a check deletes checks past retention.
func TestScamChecksPruned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewScamWarnings(db)
	c := conversation(t, db)
	now := time.Now().UTC()
	old := now.Add(-domain.CheckRetention - time.Hour)
	if err := s.Record(ctx, check(c, old, []domain.Code{}), nil, old.Add(-domain.CheckRetention)); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, check(c, now, []domain.Code{}), nil, now.Add(-domain.CheckRetention)); err != nil {
		t.Fatal(err)
	}
	if _, n, _ := s.Checked(ctx, c.ID, old.Add(-time.Hour)); n != 1 {
		t.Fatalf("checks left: %d", n)
	}
}

// Dismiss locks the row, so it never loses a race: it dismisses once,
// changes nothing again, and finds nothing for a member without a warning.
func TestScamWarningsDismiss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewScamWarnings(db)
	c := conversation(t, db)
	at := time.Now().UTC().Truncate(time.Microsecond)
	w := domain.NewWarning(c.ID, c.AccountB, c.AccountA)
	w.Raise([]domain.Code{domain.CodePrepayment}, nil, at, nil)
	if err := s.Record(ctx, check(c, at, []domain.Code{domain.CodePrepayment}), []repository.WarningWrite{{Warning: &w, WriteID: uuid.NewV7()}},
		at.Add(-domain.CheckRetention)); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.Dismiss(ctx, c.ID, c.AccountB, at.Add(time.Minute), uuid.NewV7()); err != nil || !changed {
		t.Fatalf("dismiss: %v %v", changed, err)
	}
	if changed, err := s.Dismiss(ctx, c.ID, c.AccountB, at.Add(2*time.Minute), uuid.NewV7()); err != nil || changed {
		t.Fatalf("again: %v %v", changed, err)
	}
	if got, _ := s.Get(ctx, c.ID, c.AccountB); !got.Dismissed() || !got.DismissedAt().Equal(at.Add(time.Minute)) || updates(t, db, c.AccountB) != 2 {
		t.Fatalf("%+v, %d updates", got.State(), updates(t, db, c.AccountB))
	}
	if changed, err := s.Dismiss(ctx, c.ID, c.AccountA, at, uuid.NewV7()); err != nil || changed {
		t.Fatalf("no warning: %v %v", changed, err)
	}
}
