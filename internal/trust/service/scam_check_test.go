package service_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/accounttest"
	chatdomain "github.com/shopnexus/server2/internal/chat/domain"
	chatrepo "github.com/shopnexus/server2/internal/chat/repository"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
	"github.com/shopnexus/server2/internal/trust/service"
)

// fakeClassifier gives a side prepayment_request when one of its lines
// says "cọc" and sensitive_info_request for "otp", and records what it read.
type fakeClassifier struct {
	mu      sync.Mutex
	windows []domain.Window
	err     error
}

func (*fakeClassifier) Name() string       { return "fake" }
func (*fakeClassifier) PromptVersion() int { return 0 }

func (f *fakeClassifier) Classify(_ context.Context, w domain.Window) (domain.Verdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windows = append(f.windows, w)
	if f.err != nil {
		return domain.Verdict{}, f.err
	}
	v := domain.Verdict{A: []domain.Code{}, B: []domain.Code{}}
	for _, l := range w.Lines {
		codes := &v.A
		if l.Side == domain.SideB {
			codes = &v.B
		}
		if strings.Contains(strings.ToLower(l.Text), "cọc") {
			*codes = append(*codes, domain.CodePrepayment)
		}
		if strings.Contains(strings.ToLower(l.Text), "otp") {
			*codes = append(*codes, domain.CodeSensitive, "made_up")
		}
	}
	return v, nil
}

func (f *fakeClassifier) seen() []domain.Window {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Window{}, f.windows...)
}

// fakeFacts tells new_account for every suspect, or fails.
type fakeFacts struct{ err error }

func (f fakeFacts) Signals(_ context.Context, _ uuid.UUID, _ domain.Window, now time.Time) ([]domain.Signal, error) {
	if f.err != nil {
		return nil, f.err
	}
	s, _ := domain.NewAccountSignal(now.Add(-time.Hour), now)
	return []domain.Signal{s}, nil
}

type checkEnv struct {
	db     *pgxpool.Pool
	chats  chatrepo.Chats
	scams  repository.ScamWarnings
	model  *fakeClassifier
	client *ingress.Client
	prefix string
	c      chatdomain.Conversation
}

func newCheckEnv(t *testing.T, facts service.FactSource) checkEnv {
	t.Helper()
	db := pgtest.New(t)
	chats := chatrepo.NewChats(db)
	model := &fakeClassifier{}
	prefix := restatetest.Prefix()
	trust := service.New(service.Deps{
		Scams: repository.NewScamWarnings(db), Chats: chatsvc.New(chatsvc.Deps{Chats: chats, Now: time.Now}),
		Classifier: model, Facts: []service.FactSource{facts}, Log: slog.New(slog.DiscardHandler), Prefix: prefix, Now: time.Now,
	})
	client := restatetest.Start(t, trust.Durable()...)
	c, err := chats.OpenPair(context.Background(), chatdomain.NewConversation(uuid.NewV7(), accounttest.New(t, db), accounttest.New(t, db), time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return checkEnv{db: db, chats: chats, scams: repository.NewScamWarnings(db), model: model, client: client, prefix: prefix, c: c}
}

func (e checkEnv) say(t *testing.T, sender uuid.UUID, text string) chatdomain.Message {
	t.Helper()
	m, err := chatdomain.NewMessage(uuid.NewV7(), e.c.ID, sender, text, nil, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.chats.AddMessage(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (e checkEnv) run(t *testing.T) {
	t.Helper()
	if _, err := ingress.Object[restate.Void, restate.Void](e.client, e.prefix+service.ScamCheckName, e.c.ID.String(), "Run").
		Request(context.Background(), restate.Void{}); err != nil {
		t.Fatal(err)
	}
}

func (e checkEnv) checks(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM scam_checks WHERE conversation_id = @c`,
		pgx.StrictNamedArgs{"c": e.c.ID}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The suspect's codes warn the other member, with the facts and redacted
// lines; a run with nothing new reads nothing.
func TestScamCheckWarnsTheOtherMember(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	e.say(t, e.c.AccountB, "Còn không bạn?")
	e.say(t, e.c.AccountA, "Còn, chuyển cọc vào 0912 345 678 nhé")
	e.run(t)
	w, err := e.scams.Get(context.Background(), e.c.ID, e.c.AccountB)
	if err != nil || w.SuspectID() != e.c.AccountA || len(w.Signals()) != 2 ||
		w.Signals()[0].Code != domain.CodePrepayment || w.Signals()[1].Code != domain.CodeNewAccount {
		t.Fatalf("%+v %v", w.State(), err)
	}
	if _, err = e.scams.Get(context.Background(), e.c.ID, e.c.AccountA); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("the suspect is warned: %v", err)
	}
	seen := e.model.seen()
	if len(seen) != 1 || len(seen[0].Lines) != 2 || seen[0].Lines[1].Text != "Còn, chuyển cọc vào <PHONE> nhé" ||
		seen[0].Lines[1].Side != domain.SideA {
		t.Fatalf("read %+v", seen)
	}
	e.run(t)
	if len(e.model.seen()) != 1 || e.checks(t) != 1 {
		t.Fatalf("a second run read again: %d checks", e.checks(t))
	}
}

// Without a message code nothing is raised, but the check is recorded.
func TestScamCheckFactsAloneRaiseNothing(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	e.say(t, e.c.AccountA, "Xe còn bạn ơi")
	e.run(t)
	if _, err := e.scams.Get(context.Background(), e.c.ID, e.c.AccountB); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("raised: %v", err)
	}
	if e.checks(t) != 1 {
		t.Fatalf("%d checks", e.checks(t))
	}
}

func TestScamCheckRunsAgainForANewMessage(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	e.say(t, e.c.AccountA, "Chuyển cọc trước nhé")
	e.run(t)
	e.say(t, e.c.AccountA, "Gửi mình mã OTP")
	e.run(t)
	w, _ := e.scams.Get(context.Background(), e.c.ID, e.c.AccountB)
	if seen := e.model.seen(); len(seen) != 2 || len(seen[1].Lines) != 2 || len(w.Signals()) != 3 ||
		w.Signals()[2].Code != domain.CodeSensitive {
		t.Fatalf("read %d windows, signals %+v", len(seen), w.Signals())
	}
}

// A dismissal made between two checks survives the second, and the new
// code undoes it.
func TestScamCheckKeepsADismissal(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	e.say(t, e.c.AccountA, "Chuyển cọc trước nhé")
	e.run(t)
	if _, err := e.scams.Dismiss(context.Background(), e.c.ID, e.c.AccountB, time.Now(), uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	e.say(t, e.c.AccountA, "Cọc đi bạn")
	e.run(t)
	if got, _ := e.scams.Get(context.Background(), e.c.ID, e.c.AccountB); !got.Dismissed() || got.DismissedAt() == nil {
		t.Fatalf("the same code undid the dismissal: %+v", got.State())
	}
	e.say(t, e.c.AccountA, "Gửi mình mã OTP")
	e.run(t)
	if got, _ := e.scams.Get(context.Background(), e.c.ID, e.c.AccountB); got.Dismissed() {
		t.Fatalf("a new code kept it dismissed: %+v", got.State())
	}
}

func TestScamCheckCap(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	for range domain.ChecksPerDay {
		if _, err := e.db.Exec(context.Background(), `INSERT INTO scam_checks (id, conversation_id, message_ids,
    classifier, prompt_version, codes_a, codes_b, created_at) VALUES (@id, @c, ARRAY[@id::uuid], 'fake', 0, '{}', '{}', now())`,
			pgx.StrictNamedArgs{"id": uuid.NewV7(), "c": e.c.ID}); err != nil {
			t.Fatal(err)
		}
	}
	e.say(t, e.c.AccountA, "Chuyển cọc trước nhé")
	e.run(t)
	if len(e.model.seen()) != 0 {
		t.Fatal("the capped conversation was read")
	}
}

func TestScamCheckRefused(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	e.model.err = service.ErrRefused
	e.say(t, e.c.AccountA, "Chuyển cọc trước nhé")
	e.run(t)
	if len(e.model.seen()) != 1 || e.checks(t) != 0 {
		t.Fatalf("read %d times, %d checks", len(e.model.seen()), e.checks(t))
	}
}

func TestScamCheckFactSourceFails(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{err: errors.New("catalog down")})
	e.say(t, e.c.AccountA, "Chuyển cọc trước nhé")
	e.run(t)
	if w, err := e.scams.Get(context.Background(), e.c.ID, e.c.AccountB); err != nil || len(w.Signals()) != 1 {
		t.Fatalf("%+v %v", w.State(), err)
	}
}

// A message that becomes sent after a newer one, as a photo message does
// once its photos are processed, gets a check of its own.
func TestScamCheckReadsALateMessage(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	late, err := chatdomain.NewMessage(uuid.NewV7(), e.c.ID, e.c.AccountA, "Mình chuyển dư rồi, bạn chuyển cọc lại giúp",
		[]uuid.UUID{uuid.NewV7()}, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.chats.AddMessage(context.Background(), late); err != nil {
		t.Fatal(err)
	}
	e.say(t, e.c.AccountA, "nhé bạn")
	e.run(t)
	if _, err = e.db.Exec(context.Background(), `UPDATE messages SET status = 'sent' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": late.ID()}); err != nil {
		t.Fatal(err)
	}
	e.run(t)
	seen := e.model.seen()
	if len(seen) != 2 || len(seen[1].Lines) != 2 || seen[1].Lines[0].MessageID != late.ID() {
		t.Fatalf("read %+v", seen)
	}
	if w, err := e.scams.Get(context.Background(), e.c.ID, e.c.AccountB); err != nil || w.Signals()[0].Code != domain.CodePrepayment {
		t.Fatalf("%+v %v", w.State(), err)
	}
}

// A burst of messages and the runs it sends at once end in one check: the
// object runs them one at a time and the later ones find the window read.
func TestScamCheckBurst(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	for _, text := range []string{"Còn bạn", "Giá đó nha", "Chuyển cọc trước", "Mình giữ xe", "Nhanh nhé"} {
		e.say(t, e.c.AccountA, text)
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { e.run(t) })
	}
	wg.Wait()
	if seen := e.model.seen(); len(seen) != 1 || len(seen[0].Lines) != 5 || e.checks(t) != 1 {
		t.Fatalf("read %d windows, %d checks", len(seen), e.checks(t))
	}
}

// A classifier that keeps failing is tried 3 times, then the check is
// skipped with no row.
func TestScamCheckGivesUp(t *testing.T) {
	t.Parallel()
	e := newCheckEnv(t, fakeFacts{})
	e.model.err = errors.New("model down")
	e.say(t, e.c.AccountA, "Chuyển cọc trước nhé")
	e.run(t)
	if n := len(e.model.seen()); n != 3 || e.checks(t) != 0 {
		t.Fatalf("tried %d times, %d checks", n, e.checks(t))
	}
}
