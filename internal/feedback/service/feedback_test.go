package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/feedback/repository"
	"github.com/shopnexus/server/internal/feedback/service"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

var (
	now = time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	cli = audit.ByCLI("test")
)

// newService builds feedback on a fresh database with a stopped clock;
// media is nil unless photos says so.
func newService(t *testing.T, photos bool) (*service.Feedback, func() uuid.UUID, *mediasvc.Media) {
	t.Helper()
	db := pgtest.New(t)
	var media *mediasvc.Media
	d := service.Deps{Store: repository.NewStore(db), Accounts: accounttest.Service(db), Now: func() time.Time { return now }}
	if photos {
		in, p := restatetest.Ingress(t), restatetest.Prefix()
		media = mediatest.Service(db, objectstoretest.New(t), in, p)
		restatetest.Start(t, media.Durable()...)
		d.Media = media
	}
	return service.New(d), func() uuid.UUID { return accounttest.New(t, db) }, media
}

// Send stores feedback once per key and lists it; a mood or a text is
// needed, and the day's limit holds.
func TestSendAndMine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, person, _ := newService(t, false)
	me := person()
	key := uuid.NewV7()
	in := domain.Draft{
		Mood: 4, Kind: "idea", Text: "  Thêm bộ lọc size khung ", Screen: "search_results", Contact: true,
		Device: &domain.Device{Model: "Pixel 9", OS: "Android 16", AppVersion: "2.5.0"},
	}
	v, existed, err := svc.Send(ctx, me, key, in)
	if err != nil || existed || v.Text != "Thêm bộ lọc size khung" || v.Mood != 4 || v.Kind != domain.KindIdea ||
		v.Status != domain.StatusSent || !v.StatusAt.Equal(now) || !v.CreatedAt.Equal(now) || v.Photos == nil || v.Reply != nil {
		t.Fatalf("%+v %v %v", v, existed, err)
	}
	again, existed, err := svc.Send(ctx, me, key, domain.Draft{Mood: 1})
	if err != nil || !existed || again.ID != v.ID || again.Mood != 4 {
		t.Fatalf("a repeated key: %+v %v %v", again, existed, err)
	}
	if _, _, err = svc.Send(ctx, me, uuid.NewV7(), domain.Draft{Text: "   "}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("neither mood nor text: %v", err)
	}
	for range domain.DailyLimit - 1 {
		if _, _, err = svc.Send(ctx, me, uuid.NewV7(), domain.Draft{Mood: 3}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = svc.Send(ctx, me, uuid.NewV7(), domain.Draft{Mood: 3}); !errors.Is(err, domain.ErrTooMany) {
		t.Fatalf("the 21st: %v", err)
	}
	// A key the account used still answers past the limit.
	if _, existed, err = svc.Send(ctx, me, key, in); err != nil || !existed {
		t.Fatalf("a repeat past the limit: %v %v", existed, err)
	}
	mine, err := svc.Mine(ctx, me)
	if err != nil || len(mine) != domain.DailyLimit || mine[len(mine)-1].ID != v.ID {
		t.Fatalf("mine: %d %v", len(mine), err)
	}
	if others, _ := svc.Mine(ctx, person()); len(others) != 0 {
		t.Fatalf("another's: %+v", others)
	}
}

// Feedback from the website keeps its page and a browser without an app
// version, for staff.
func TestSendFromTheWebsite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, person, _ := newService(t, false)
	v, _, err := svc.Send(ctx, person(), uuid.NewV7(), domain.Draft{
		Mood: 2, Screen: "search", Source: "web", PageURL: "https://shopnexus.id.vn/search?q=laptop",
		Device: &domain.Device{Model: "Chrome 132", OS: "Windows 11"},
	})
	if err != nil {
		t.Fatal(err)
	}
	q, err := svc.Queue(ctx, nil, nil, page.Request{Limit: 5})
	if err != nil || len(q.Items) != 1 {
		t.Fatalf("queue: %+v %v", q, err)
	}
	c := q.Items[0]
	if c.Feedback.ID != v.ID || c.Source != domain.SourceWeb || c.PageURL != "https://shopnexus.id.vn/search?q=laptop" ||
		c.Device == nil || c.Device.Model != "Chrome 132" || c.Device.AppVersion != "" {
		t.Fatalf("%+v", c)
	}
}

// Screenshots are processed through media, kept in order, and refused
// when they are another's.
func TestSendPhotos(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, person, media := newService(t, true)
	me := person()
	a := mediatest.Upload(t, media, me, "image/jpeg", mediatest.JPEG(t, 800, 600))
	b := mediatest.Upload(t, media, me, "image/jpeg", mediatest.JPEG(t, 600, 800))
	key := uuid.NewV7()
	v, _, err := svc.Send(ctx, me, key, domain.Draft{Text: "Nút Lưu bị che", Kind: "bug", Photos: []uuid.UUID{b, a}})
	if err != nil || len(v.Photos) != 2 || v.Photos[0].UploadID != b || v.Photos[1].UploadID != a ||
		!strings.Contains(v.Photos[0].URL, "media/") || v.Photos[0].URL == v.Photos[0].ThumbURL {
		t.Fatalf("%+v %v", v.Photos, err)
	}
	theirs := mediatest.Upload(t, media, person(), "image/jpeg", mediatest.JPEG(t, 400, 400))
	if _, _, err = svc.Send(ctx, me, uuid.NewV7(), domain.Draft{Mood: 1, Photos: []uuid.UUID{theirs}}); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("another's photo: %v", err)
	}
}

// Staff move a group, reply, heart and join; Facts reads what a notice
// says, and a deleted account's feedback leaves its group.
func TestStaff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, person, _ := newService(t, false)
	owner, other := person(), person()
	mine, _, err := svc.Send(ctx, owner, uuid.NewV7(), domain.Draft{Kind: "idea", Text: "Lọc theo size khung"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, _, err := svc.Send(ctx, other, uuid.NewV7(), domain.Draft{Kind: "idea", Text: "Cần lọc size xe đạp"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.SameAs(ctx, theirs.ID, mine.ID, cli)
	if err != nil || c.RootID != mine.ID || c.Feedback.Others != 1 || c.AccountID != other {
		t.Fatalf("same as: %+v %v", c, err)
	}
	if _, err = svc.SameAs(ctx, mine.ID, mine.ID, cli); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("itself: %v", err)
	}
	if _, err = svc.SetStatus(ctx, mine.ID, service.StatusInput{Status: "in_progress", Version: "2.6"}, cli); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a version without shipped: %v", err)
	}
	if c, err = svc.SetStatus(ctx, theirs.ID, service.StatusInput{Status: "shipped", Note: "Bộ lọc Size khung đã có", Version: "2.5"}, cli); err != nil ||
		c.Feedback.Status != domain.StatusShipped || c.Feedback.ShippedVersion != "2.5" {
		t.Fatalf("shipped: %+v %v", c, err)
	}
	f, found, err := svc.Facts(ctx, mine.ID)
	if err != nil || !found || f.Owner != owner || f.Status != domain.StatusShipped || f.Version != "2.5" {
		t.Fatalf("facts: %+v %v %v", f, found, err)
	}
	if _, err = svc.Reply(ctx, mine.ID, "  ", cli); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a blank reply: %v", err)
	}
	if c, err = svc.Reply(ctx, mine.ID, " Cảm ơn bạn ", cli); err != nil || c.Feedback.Reply == nil || c.Feedback.Reply.Text != "Cảm ơn bạn" {
		t.Fatalf("reply: %+v %v", c, err)
	}
	if c, err = svc.Heart(ctx, mine.ID, true, cli); err != nil || !c.Feedback.Hearted {
		t.Fatalf("heart: %+v %v", c, err)
	}
	shipped := domain.StatusShipped
	q, err := svc.Queue(ctx, &shipped, nil, page.Request{Limit: 1})
	if err != nil || len(q.Items) != 1 || q.Items[0].Feedback.ID != theirs.ID || q.NextCursor == nil {
		t.Fatalf("queue: %+v %v", q, err)
	}
	d, err := svc.Detail(ctx, theirs.ID)
	if err != nil || d.Item.Feedback.ID != theirs.ID || d.Item.Person.ID != other || d.Item.Person.Status != "active" ||
		len(d.Group) != 1 || d.Group[0].Feedback.ID != mine.ID {
		t.Fatalf("detail: %+v %v", d, err)
	}
	gs, err := svc.Groups(ctx, nil)
	if err != nil || len(gs) != 1 || gs[0].Root.Feedback.ID != mine.ID || gs[0].Size != 2 || gs[0].Root.Person.ID != owner {
		t.Fatalf("groups: %+v %v", gs, err)
	}
	if m, sumErr := svc.Summary(ctx); sumErr != nil || m.Sent != 2 || m.Ideas != 2 || m.ShippedGroups != 1 || len(m.DaySent) != service.SummaryDays {
		t.Fatalf("summary: %+v %v", m, sumErr)
	}
	if err = svc.AccountDeleted(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, found, err = svc.Facts(ctx, mine.ID); err != nil || found {
		t.Fatalf("after deletion: %v %v", found, err)
	}
	left, _ := svc.Mine(ctx, other)
	if len(left) != 1 || left[0].Others != 0 || left[0].Status != domain.StatusShipped {
		t.Fatalf("the other's: %+v", left)
	}
}
