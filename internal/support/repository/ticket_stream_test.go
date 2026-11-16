package repository_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/stream"
	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
)

func TestTicketUpdatesAreOwnerOnlyAndReplaySafe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	owner := accounttest.New(t, db)
	row := ticket(t, db, owner, domain.KindOther, nil, "")
	key := uuid.NewV7()
	tk, _, err := s.Insert(ctx, row, key, "", at.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Insert(ctx, row, key, "", at.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	initial := message(t, db, tk.ConversationID, owner)
	if err = s.SetFirstMessage(ctx, tk.ID, initial); err != nil {
		t.Fatal(err)
	}
	if err = s.SetFirstMessage(ctx, tk.ID, initial); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, tk.ID, nil, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if reopened, recvErr := s.Messaged(ctx, tk.ConversationID, initial, domain.FromPerson, at); recvErr != nil || reopened {
		t.Fatalf("late initial message: %v %v", reopened, recvErr)
	}
	next := uuid.NewV7()
	if reopened, recvErr := s.Messaged(ctx, tk.ConversationID, next, domain.FromPerson, at.Add(2*time.Minute)); recvErr != nil || !reopened {
		t.Fatalf("new reply: %v %v", reopened, recvErr)
	}
	if _, err = s.Resolve(ctx, tk.ID, nil, at.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if reopened, recvErr := s.Messaged(ctx, tk.ConversationID, next, domain.FromPerson, at.Add(2*time.Minute)); recvErr != nil || reopened {
		t.Fatalf("replayed reply: %v %v", reopened, recvErr)
	}
	if _, err = s.Reopen(ctx, tk.ID, nil, at.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveAllOf(ctx, owner, at.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	log := stream.PGLog{DB: db}
	updates, err := log.Since(ctx, owner, 0, 100)
	if err != nil || len(updates.Updates) != 7 || updates.Reset {
		t.Fatalf("updates: %+v %v", updates, err)
	}
	for i, u := range updates.Updates {
		if u.Kind != stream.KindTicket || u.ConversationID == nil || *u.ConversationID != tk.ConversationID || u.Seq != int64(i+1) {
			t.Fatalf("hint: %+v", u)
		}
	}
	for _, other := range []uuid.UUID{accountdomain.ShopNexusID, accounttest.New(t, db)} {
		if seq, seqErr := log.Current(ctx, other); seqErr != nil || seq != 0 {
			t.Fatalf("other account notified: %d %v", seq, seqErr)
		}
	}
}

func TestTicketNotifyFailureRollsBackStatusEventAndReceipt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	owner := accounttest.New(t, db)
	tk := open(t, s, ticket(t, db, owner, domain.KindOther, nil, ""))
	if _, err := s.Resolve(ctx, tk.ID, nil, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `CREATE FUNCTION refuse_ticket_hint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test hint failure'; END $$;
CREATE TRIGGER refuse_ticket_hint BEFORE INSERT ON account_updates FOR EACH ROW EXECUTE FUNCTION refuse_ticket_hint()`); err != nil {
		t.Fatal(err)
	}
	msg := uuid.NewV7()
	if _, err := s.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, at.Add(time.Minute)); err == nil {
		t.Fatal("notification failure accepted")
	}
	got, err := s.Get(ctx, tk.ID)
	if err != nil || got.Status != domain.StatusResolved || len(got.Events) != 2 {
		t.Fatalf("rolled-back ticket: %+v %v", got, err)
	}
	var receipts int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM ticket_message_receipts WHERE message_id = @id`, pgx.StrictNamedArgs{"id": msg}).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("receipt: %d %v", receipts, err)
	}
	log := stream.PGLog{DB: db}
	if seq, seqErr := log.Current(ctx, owner); seqErr != nil || seq != 2 {
		t.Fatalf("rollback seq: %d %v", seq, seqErr)
	}
	if _, err = db.Exec(ctx, `DROP TRIGGER refuse_ticket_hint ON account_updates`); err != nil {
		t.Fatal(err)
	}
	if reopened, retryErr := s.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, at.Add(time.Minute)); retryErr != nil || !reopened {
		t.Fatalf("retry: %v %v", reopened, retryErr)
	}
	if seq, seqErr := log.Current(ctx, owner); seqErr != nil || seq != 3 {
		t.Fatalf("retry seq: %d %v", seq, seqErr)
	}
}

func TestTicketMessageReplayCannotUndoConcurrentResolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	owner := accounttest.New(t, db)
	tk := open(t, s, ticket(t, db, owner, domain.KindOther, nil, ""))
	if _, err := s.Resolve(ctx, tk.ID, nil, at); err != nil {
		t.Fatal(err)
	}
	msg := uuid.NewV7()
	if _, err := s.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() { <-start; _, err := s.Resolve(ctx, tk.ID, nil, at.Add(2*time.Minute)); errs <- err })
	wg.Go(func() {
		<-start
		_, err := s.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, at.Add(time.Minute))
		errs <- err
	})
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(ctx, tk.ID)
	if err != nil || got.Status != domain.StatusResolved || len(got.Events) != 4 {
		t.Fatalf("status race: %+v %v", got, err)
	}
	log := stream.PGLog{DB: db}
	if seq, seqErr := log.Current(ctx, owner); seqErr != nil || seq != 4 {
		t.Fatalf("status hints: %d %v", seq, seqErr)
	}
}

func messageAt(t *testing.T, db *pgxpool.Pool, conversation, sender uuid.UUID, sentAt time.Time) uuid.UUID {
	t.Helper()
	id := message(t, db, conversation, sender)
	if _, err := db.Exec(t.Context(), `UPDATE messages SET created_at = @at WHERE id = @id`, pgx.StrictNamedArgs{"at": sentAt, "id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDelayedTicketMessageCannotUndoResolution(t *testing.T) {
	t.Parallel()
	for _, deleted := range []bool{false, true} {
		name := "moderator"
		if deleted {
			name = "account_deleted"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db := pgtest.New(t)
			repo := repository.NewTickets(db)
			owner := accounttest.New(t, db)
			tk := open(t, repo, ticket(t, db, owner, domain.KindOther, nil, ""))
			msgAt, resolvedAt := at.Add(time.Minute), at.Add(2*time.Minute)
			msg := messageAt(t, db, tk.ConversationID, owner, msgAt)
			if deleted {
				if err := accounttest.Service(db).Erase(ctx, owner); err != nil {
					t.Fatal(err)
				}
				if err := repo.ResolveAllOf(ctx, owner, resolvedAt); err != nil {
					t.Fatal(err)
				}
			} else if _, err := repo.Resolve(ctx, tk.ID, actor(member(t, db, "Lan")), resolvedAt); err != nil {
				t.Fatal(err)
			}
			reopened, err := repo.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, msgAt)
			if err != nil || reopened {
				t.Errorf("late first delivery reopened: %v %v", reopened, err)
			}
			current, err := repo.Get(ctx, tk.ID)
			if err != nil || current.Status != domain.StatusResolved || current.ResolvedAt == nil || !current.ResolvedAt.Equal(resolvedAt) || len(current.Events) != 2 {
				t.Errorf("resolution lost: %+v %v", current, err)
			}
			log := stream.PGLog{DB: db}
			if seq, seqErr := log.Current(ctx, owner); seqErr != nil || seq != 2 {
				t.Errorf("stale status hint: %d %v", seq, seqErr)
			}
			if _, err = repo.Messaged(ctx, tk.ConversationID, uuid.NewV7(), domain.FromPerson, resolvedAt); err != nil {
				t.Fatal(err)
			}
			if current, err = repo.Get(ctx, tk.ID); err != nil || current.Status != domain.StatusResolved {
				t.Errorf("equal timestamp undid resolution: %+v %v", current, err)
			}
			reopened, err = repo.Messaged(ctx, tk.ConversationID, uuid.NewV7(), domain.FromPerson, resolvedAt.Add(time.Minute))
			if err != nil || reopened == deleted {
				t.Errorf("new message, deleted=%v: %v %v", deleted, reopened, err)
			}
		})
	}
}

func TestDelayedInitialMessageMetadataInEitherHandlerOrder(t *testing.T) {
	t.Parallel()
	for _, workerFirst := range []bool{false, true} {
		name := "attach_first"
		if workerFirst {
			name = "worker_first"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db := pgtest.New(t)
			repo := repository.NewTickets(db)
			owner := accounttest.New(t, db)
			tk := open(t, repo, ticket(t, db, owner, domain.KindOther, nil, ""))
			sentAt := at.Add(time.Hour)
			msg := messageAt(t, db, tk.ConversationID, owner, sentAt)
			if workerFirst {
				if _, err := repo.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, sentAt); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.SetFirstMessage(ctx, tk.ID, msg); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, sentAt); err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(ctx, tk.ID)
			if err != nil || !got.LastMessageAt.Equal(sentAt) || got.LastFrom != domain.FromPerson || got.FirstMessageID == nil || *got.FirstMessageID != msg {
				t.Fatalf("initial metadata: %+v %v", got, err)
			}
		})
	}
}

func TestInitialAttachmentKeepsNewerReplyMetadata(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := pgtest.New(t)
	repo := repository.NewTickets(db)
	owner := accounttest.New(t, db)
	tk := open(t, repo, ticket(t, db, owner, domain.KindOther, nil, ""))
	initialAt, replyAt := at.Add(time.Minute), at.Add(time.Hour)
	initial := messageAt(t, db, tk.ConversationID, owner, initialAt)
	reply := messageAt(t, db, tk.ConversationID, accountdomain.ShopNexusID, replyAt)
	if _, err := repo.Messaged(ctx, tk.ConversationID, reply, domain.FromShopNexus, replyAt); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetFirstMessage(ctx, tk.ID, initial); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, tk.ID)
	if err != nil || !got.LastMessageAt.Equal(replyAt) || got.LastFrom != domain.FromShopNexus {
		t.Fatalf("newer reply overwritten: %+v %v", got, err)
	}
}

func TestInitialMessageAfterResolutionReopensInEitherHandlerOrder(t *testing.T) {
	t.Parallel()
	for _, workerFirst := range []bool{false, true} {
		name := "attach_first"
		if workerFirst {
			name = "worker_first"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db := pgtest.New(t)
			repo := repository.NewTickets(db)
			owner := accounttest.New(t, db)
			tk := open(t, repo, ticket(t, db, owner, domain.KindOther, nil, ""))
			if _, err := repo.Resolve(ctx, tk.ID, nil, at.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			sentAt := at.Add(time.Hour)
			msg := messageAt(t, db, tk.ConversationID, owner, sentAt)
			if workerFirst {
				if _, err := repo.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, sentAt); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.SetFirstMessage(ctx, tk.ID, msg); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Messaged(ctx, tk.ConversationID, msg, domain.FromPerson, sentAt); err != nil {
				t.Fatal(err)
			}
			got, err := repo.Get(ctx, tk.ID)
			if err != nil || got.Status != domain.StatusOpen || !got.LastMessageAt.Equal(sentAt) || len(got.Events) != 3 {
				t.Fatalf("initial reopen ordering: %+v %v", got, err)
			}
		})
	}
}

func TestInitialAttachmentRollsBackMetadataWithFailedHint(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := pgtest.New(t)
	repo := repository.NewTickets(db)
	owner := accounttest.New(t, db)
	tk := open(t, repo, ticket(t, db, owner, domain.KindOther, nil, ""))
	sentAt := at.Add(time.Hour)
	msg := messageAt(t, db, tk.ConversationID, owner, sentAt)
	if _, err := db.Exec(ctx, `CREATE FUNCTION refuse_ticket_hint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test hint failure'; END $$;
CREATE TRIGGER refuse_ticket_hint BEFORE INSERT ON account_updates FOR EACH ROW EXECUTE FUNCTION refuse_ticket_hint()`); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetFirstMessage(ctx, tk.ID, msg); err == nil {
		t.Fatal("attachment ignored notification failure")
	}
	got, err := repo.Get(ctx, tk.ID)
	if err != nil || got.FirstMessageID != nil || !got.LastMessageAt.Equal(at) {
		t.Fatalf("metadata survived rollback: %+v %v", got, err)
	}
	var receipts int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM ticket_message_receipts WHERE message_id = @id`, pgx.StrictNamedArgs{"id": msg}).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("receipt survived rollback: %d %v", receipts, err)
	}
	if _, err = db.Exec(ctx, `DROP TRIGGER refuse_ticket_hint ON account_updates`); err != nil {
		t.Fatal(err)
	}
	if err = repo.SetFirstMessage(ctx, tk.ID, msg); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(ctx, tk.ID)
	if err != nil || got.FirstMessageID == nil || !got.LastMessageAt.Equal(sentAt) {
		t.Fatalf("attachment retry: %+v %v", got, err)
	}
}
