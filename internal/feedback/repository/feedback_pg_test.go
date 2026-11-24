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
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/feedback/repository"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

var (
	at  = time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	cli = audit.ByCLI("test")
)

// send stores account's text feedback under a new key, made at the
// tests' time.
func send(t *testing.T, s repository.Store, account uuid.UUID, text string) repository.Feedback {
	t.Helper()
	f, existed, err := s.Insert(context.Background(), repository.Feedback{
		ID: uuid.NewV7(), AccountID: account, Text: text, Contact: true, CreatedAt: at,
	}, uuid.NewV7(), at.Add(-domain.Window))
	if err != nil || existed {
		t.Fatalf("%v %v", existed, err)
	}
	return f
}

func actions(t *testing.T, db *pgxpool.Pool, target uuid.UUID) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT action_kind FROM console_actions WHERE target_id = @id ORDER BY id`,
		pgx.StrictNamedArgs{"id": target})
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Insert keeps every field, returns the key's feedback on a repeat, stops
// at the day's limit and refuses a deleted account.
func TestInsert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewStore(db)
	me := accounttest.New(t, db)
	key, upload := uuid.NewV7(), uuid.NewV7()
	in := repository.Feedback{
		ID: uuid.NewV7(), AccountID: me, Mood: 2, Kind: domain.KindBug, Text: "Lọc giá không chạy",
		Photos: []domain.Photo{{UploadID: upload, PhotoKey: "media/a.jpg", ThumbKey: "media/a_thumb.jpg"}},
		Device: &domain.Device{Model: "Pixel 9", OS: "Android 16", AppVersion: "2.5.0"}, Screen: "search_results", Contact: true,
		CreatedAt: at,
	}
	f, existed, err := s.Insert(ctx, in, key, at.Add(-domain.Window))
	if err != nil || existed || f.Mood != 2 || f.Kind != domain.KindBug || f.Text != in.Text || len(f.Photos) != 1 ||
		f.Photos[0] != in.Photos[0] || *f.Device != *in.Device || f.Screen != "search_results" || !f.Contact ||
		f.Status != domain.StatusSent || !f.StatusAt.Equal(at) || f.RootID != f.ID || f.Others != 0 || f.RepliedAt != nil {
		t.Fatalf("%+v %v %v", f, existed, err)
	}
	again := in
	again.ID = uuid.NewV7()
	if f, existed, err = s.Insert(ctx, again, key, at.Add(-domain.Window)); err != nil || !existed || f.ID != in.ID {
		t.Fatalf("a repeated key: %+v %v %v", f.ID, existed, err)
	}
	if f, err = s.ByKey(ctx, me, key); err != nil || f.ID != in.ID {
		t.Fatalf("by key: %v", err)
	}
	if _, err = s.ByKey(ctx, accounttest.New(t, db), key); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another's key: %v", err)
	}
	mood, _, err := s.Insert(ctx, repository.Feedback{ID: uuid.NewV7(), AccountID: me, Mood: 5, CreatedAt: at}, uuid.NewV7(), at.Add(-domain.Window))
	if err != nil || mood.Device != nil || mood.Text != "" || mood.Kind != "" || mood.Photos == nil || len(mood.Photos) != 0 {
		t.Fatalf("a mood alone: %+v %v", mood, err)
	}
	if n, countErr := s.SentSince(ctx, me, at.Add(-time.Hour)); countErr != nil || n != 2 {
		t.Fatalf("sent since: %d %v", n, countErr)
	}
}

func TestInsertLimitAndTombstone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewStore(db)
	me := accounttest.New(t, db)
	for range domain.DailyLimit {
		send(t, s, me, "x")
	}
	_, _, err := s.Insert(ctx, repository.Feedback{ID: uuid.NewV7(), AccountID: me, Text: "x", CreatedAt: at}, uuid.NewV7(), at.Add(-domain.Window))
	if !errors.Is(err, domain.ErrTooMany) {
		t.Fatalf("over the limit: %v", err)
	}
	// Feedback older than the window no longer counts.
	if _, _, err = s.Insert(ctx, repository.Feedback{ID: uuid.NewV7(), AccountID: me, Text: "x", CreatedAt: at}, uuid.NewV7(), at); err != nil {
		t.Fatalf("a later day: %v", err)
	}
	gone := accounttest.New(t, db)
	if _, err = db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Insert(ctx, repository.Feedback{ID: uuid.NewV7(), AccountID: gone, Text: "x", CreatedAt: at}, uuid.NewV7(), at)
	if !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("a deleted account: %v", err)
	}
}

func TestOfAccountAndQueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewStore(db)
	me := accounttest.New(t, db)
	a, b, c := send(t, s, me, "a"), send(t, s, me, "b"), send(t, s, accounttest.New(t, db), "c")
	mine, err := s.OfAccount(ctx, me, 10)
	if err != nil || len(mine) != 2 || mine[0].ID != b.ID || mine[1].ID != a.ID {
		t.Fatalf("mine: %+v %v", mine, err)
	}
	if _, err = s.SetStatus(ctx, a.ID, domain.StatusChange{Status: domain.StatusSeen}, cli, at); err != nil {
		t.Fatal(err)
	}
	all, err := s.Queue(ctx, nil, nil, page.Request{Limit: 2})
	if err != nil || len(all) != 3 || all[0].ID != c.ID || all[1].ID != b.ID {
		t.Fatalf("queue: %+v %v", all, err)
	}
	seen := domain.StatusSeen
	if got, _ := s.Queue(ctx, &seen, nil, page.Request{}); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("seen: %+v", got)
	}
	if got, _ := s.Queue(ctx, nil, nil, page.Request{Cursor: b.ID}); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("below b: %+v", got)
	}
}

// A status moves the whole group, tells each owner whose status moved to
// in_progress or shipped, and is audited; a member joins with its root
// moving its members, and they take the group's status.
func TestGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewStore(db)
	root, member, other, lone := send(t, s, accounttest.New(t, db), "root"), send(t, s, accounttest.New(t, db), "member"),
		send(t, s, accounttest.New(t, db), "other"), send(t, s, accounttest.New(t, db), "lone")
	f, err := s.Join(ctx, member.ID, root.ID, cli, at)
	if err != nil || f.RootID != root.ID || f.Others != 1 {
		t.Fatalf("join: %+v %v", f, err)
	}
	later := at.Add(time.Hour)
	inProgress := domain.StatusChange{Status: domain.StatusInProgress, Note: "Dự kiến bản 2.6"}
	if f, err = s.SetStatus(ctx, member.ID, inProgress, cli, later); err != nil || f.Status != domain.StatusInProgress ||
		f.StatusNote != "Dự kiến bản 2.6" || !f.StatusAt.Equal(later) {
		t.Fatalf("set on a member: %+v %v", f, err)
	}
	if r, _ := s.Get(ctx, root.ID); r.Status != domain.StatusInProgress || r.Others != 1 {
		t.Fatalf("the root: %+v", r)
	}
	for _, id := range []uuid.UUID{root.ID, member.ID} {
		if kinds := pgtest.EventKinds(t, db, id); !slices.Equal(kinds, []string{"feedback.moved"}) {
			t.Fatalf("%s events %v", id, kinds)
		}
	}
	// The same status with another note moves no one.
	if f, err = s.SetStatus(ctx, root.ID, domain.StatusChange{Status: domain.StatusInProgress, Note: "Bản 2.6"}, cli, later.Add(time.Hour)); err != nil ||
		!f.StatusAt.Equal(later) || len(pgtest.EventKinds(t, db, root.ID)) != 1 {
		t.Fatalf("a new note: %+v %v", f, err)
	}
	// other is a root with lone joined to it; joining other moves both, and
	// they take the group's status, which tells their owners.
	if _, err = s.Join(ctx, lone.ID, other.ID, cli, at); err != nil {
		t.Fatal(err)
	}
	if f, err = s.Join(ctx, other.ID, member.ID, cli, later); err != nil || f.RootID != root.ID || f.Others != 3 ||
		f.Status != domain.StatusInProgress || f.StatusNote != "Bản 2.6" {
		t.Fatalf("join a root: %+v %v", f, err)
	}
	if l, _ := s.Get(ctx, lone.ID); l.RootID != root.ID || l.Status != domain.StatusInProgress {
		t.Fatalf("its member: %+v", l)
	}
	if kinds := pgtest.EventKinds(t, db, lone.ID); !slices.Equal(kinds, []string{"feedback.moved"}) {
		t.Fatalf("lone's events %v", kinds)
	}
	// Already one group changes nothing.
	if _, err = s.Join(ctx, lone.ID, root.ID, cli, later); err != nil {
		t.Fatal(err)
	}
	if got := actions(t, db, lone.ID); !slices.Equal(got, []string{"feedback.same_as"}) {
		t.Fatalf("lone's audit %v", got)
	}
	if got := actions(t, db, member.ID); !slices.Equal(got, []string{"feedback.same_as", "feedback.status"}) {
		t.Fatalf("member's audit %v", got)
	}
	shipped := domain.StatusChange{Status: domain.StatusShipped, Note: "Bộ lọc Size khung đã có", Version: "2.5"}
	if f, err = s.SetStatus(ctx, lone.ID, shipped, cli, later); err != nil || f.ShippedVersion != "2.5" {
		t.Fatalf("shipped: %+v %v", f, err)
	}
	if kinds := pgtest.EventKinds(t, db, other.ID); len(kinds) != 2 {
		t.Fatalf("other's events %v", kinds)
	}
	if _, err = s.SetStatus(ctx, uuid.NewV7(), shipped, cli, at); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err = s.Join(ctx, root.ID, uuid.NewV7(), cli, at); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("join an unknown: %v", err)
	}
}

// The first reply writes feedback.replied; a second replaces the text and
// writes none. A heart is audited once per change.
func TestReplyAndHeart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewStore(db)
	f := send(t, s, accounttest.New(t, db), "Cảm ơn ShopNexus")
	got, err := s.Reply(ctx, f.ID, "Cảm ơn bạn", cli, at)
	if err != nil || got.Reply != "Cảm ơn bạn" || got.RepliedAt == nil || !got.RepliedAt.Equal(at) {
		t.Fatalf("reply: %+v %v", got, err)
	}
	if got, err = s.Reply(ctx, f.ID, "Cảm ơn bạn nhiều", cli, at.Add(time.Minute)); err != nil || got.Reply != "Cảm ơn bạn nhiều" {
		t.Fatalf("again: %+v %v", got, err)
	}
	if kinds := pgtest.EventKinds(t, db, f.ID); !slices.Equal(kinds, []string{"feedback.replied"}) {
		t.Fatalf("events %v", kinds)
	}
	for _, h := range []bool{true, true, false} {
		if got, err = s.Heart(ctx, f.ID, h, cli, at); err != nil || got.Hearted != h {
			t.Fatalf("heart %v: %+v %v", h, got, err)
		}
	}
	if got := actions(t, db, f.ID); !slices.Equal(got, []string{"feedback.reply", "feedback.reply", "feedback.heart", "feedback.unheart"}) {
		t.Fatalf("audit %v", got)
	}
	if _, err = s.Reply(ctx, uuid.NewV7(), "x", cli, at); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("reply to none: %v", err)
	}
	if _, err = s.Heart(ctx, uuid.NewV7(), true, cli, at); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("heart none: %v", err)
	}
}

// Forget deletes the account's feedback; a group it led keeps the rest,
// under the oldest of them.
func TestForget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewStore(db)
	gone, kept := accounttest.New(t, db), accounttest.New(t, db)
	root, alsoGone := send(t, s, gone, "root"), send(t, s, gone, "mine too")
	heir, last, elsewhere := send(t, s, kept, "heir"), send(t, s, kept, "last"), send(t, s, kept, "elsewhere")
	for _, id := range []uuid.UUID{alsoGone.ID, heir.ID, last.ID} {
		if _, err := s.Join(ctx, id, root.ID, cli, at); err != nil {
			t.Fatal(err)
		}
	}
	// gone's feedback also sits in another's group.
	other := send(t, s, gone, "in another's group")
	if _, err := s.Join(ctx, other.ID, elsewhere.ID, cli, at); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(ctx, gone); err != nil {
		t.Fatal(err)
	}
	if mine, _ := s.OfAccount(ctx, gone, 10); len(mine) != 0 {
		t.Fatalf("left: %+v", mine)
	}
	h, _ := s.Get(ctx, heir.ID)
	l, _ := s.Get(ctx, last.ID)
	e, _ := s.Get(ctx, elsewhere.ID)
	if h.RootID != heir.ID || h.Others != 1 || l.RootID != heir.ID || e.RootID != elsewhere.ID || e.Others != 0 {
		t.Fatalf("heir %+v, last %+v, elsewhere %+v", h, l, e)
	}
	if err := s.Forget(ctx, gone); err != nil {
		t.Fatalf("twice: %v", err)
	}
}

// Group reads a group with its root, Groups lists roots largest first
// with their figures, and Summary counts a period against the one before.
func TestGroupFigures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewStore(db)
	put := func(mood int, kind domain.Kind, source domain.Source, screen string, made time.Time) repository.Feedback {
		f, _, err := s.Insert(ctx, repository.Feedback{
			ID: uuid.NewV7(), AccountID: accounttest.New(t, db), Mood: mood, Kind: kind, Source: source, Screen: screen, CreatedAt: made,
		}, uuid.NewV7(), made.Add(-domain.Window))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	day := 24 * time.Hour
	root := put(4, domain.KindIdea, domain.SourceWeb, "/search", at.Add(-3*day))
	a, b := put(5, domain.KindIdea, domain.SourceApp, "SearchResults", at.Add(-time.Hour)), put(4, domain.KindIdea, domain.SourceWeb, "/search", at.Add(-2*time.Hour))
	lone := put(2, domain.KindBug, domain.SourceApp, "Map", at.Add(-40*day))
	for _, f := range []repository.Feedback{a, b} {
		if _, err := s.Join(ctx, f.ID, root.ID, cli, at); err != nil {
			t.Fatal(err)
		}
	}
	group, err := s.Group(ctx, root.ID, 10)
	if err != nil || len(group) != 3 || group[0].ID != b.ID || group[2].ID != root.ID {
		t.Fatalf("group: %+v %v", group, err)
	}
	gs, err := s.Groups(ctx, nil, 10, at)
	if err != nil || len(gs) != 2 || gs[0].Root.ID != root.ID || gs[0].Size != 3 || gs[0].Moods != [5]int{0, 0, 0, 2, 1} ||
		gs[0].App != 1 || gs[0].Web != 2 || gs[0].Days != [7]int{0, 0, 0, 0, 1, 0, 2} || gs[1].Root.ID != lone.ID {
		t.Fatalf("groups: %+v %v", gs, err)
	}
	bug := domain.KindBug
	if gs, _ = s.Groups(ctx, &bug, 10, at); len(gs) != 1 || gs[0].Root.ID != lone.ID {
		t.Fatalf("bugs: %+v", gs)
	}
	if got, _ := s.Queue(ctx, nil, &bug, page.Request{}); len(got) != 1 || got[0].ID != lone.ID {
		t.Fatalf("bug queue: %+v", got)
	}
	if _, err = s.SetStatus(ctx, root.ID, domain.StatusChange{Status: domain.StatusShipped}, cli, at.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	m, err := s.Summary(ctx, at.Add(-30*day), at)
	if err != nil || m.Sent != 3 || m.App != 1 || m.Web != 2 || m.Ideas != 3 || m.Bugs != 0 || m.Mood < 4.3 || m.Mood > 4.4 ||
		m.MoodBefore != 2 || m.Waiting != 1 || m.ShippedGroups != 1 || m.ShippedPeople != 3 || len(m.DayMood) != 30 ||
		m.DaySent[29] != 2 || m.DaySent[27] != 1 || len(m.Screens) != 2 || m.Screens[0] != (repository.Screen{Screen: "/search", Count: 2}) {
		t.Fatalf("summary: %+v %v", m, err)
	}
}
