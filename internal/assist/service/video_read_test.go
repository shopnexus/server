package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	aidomain "github.com/shopnexus/server/internal/ai/domain"
	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	mediarepo "github.com/shopnexus/server/internal/media/repository"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/provider/llm"
)

// A read goes through frames and the text, notifies after each step, and
// consumes the video.
//
// UC-LST-01.
func TestReadFillsADraft(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller := accounttest.WithPhone(t, e.db)
	started := e.start(t, seller)
	if started.Status != domain.StatusReading || started.Draft != nil {
		t.Fatalf("started: %+v", started)
	}
	v := e.end(t, seller, started.ID)
	if v.Status != domain.StatusDone || v.Failure != "" || len(v.Photos) != 3 {
		t.Fatalf("%+v", v)
	}
	for _, f := range v.Photos {
		if f.URL == "" || f.UploadID == (uuid.UUID{}) {
			t.Fatalf("frame %+v", f)
		}
	}
	if v.Source != domain.SourceVideo || v.Photos[0].AtSeconds == nil {
		t.Fatalf("video read view %+v", v)
	}
	d := v.Draft
	if d == nil || d.Title != "Xe đạp Asama xanh" || d.CategoryCode != "vehicles.bicycle" ||
		d.Condition != catalogdomain.ConditionUsed || d.Price != nil || d.AllowOffers || len(d.Specs) != 0 ||
		d.Description != "Xe đi ít, có thương lượng." || len(d.Photos) != 3 ||
		d.Photos[0] != v.Photos[2].UploadID || d.Photos[1] != v.Photos[0].UploadID || d.Photos[2] != v.Photos[1].UploadID {
		t.Fatalf("draft %+v", d)
	}
	calls, images, user := e.model.asked()
	if calls != 1 || images != 3 || !strings.Contains(user, transcript) {
		t.Fatalf("model asked %d times, %d images, %q", calls, images, user)
	}
	if n := e.stt.called(); n != 1 {
		t.Fatalf("speech to text ran %d times", n)
	}
	if n := e.updates(t, seller, v.ID); n != 3 {
		t.Fatalf("%d updates, want start, frames and done", n)
	}
	if s := e.videoOf(t, v.ID); s != mediadomain.StatusConsumed {
		t.Fatalf("video %s", s)
	}
}

// Each way a read ends early is a failure on the read, and the video is
// consumed on every path.
//
// UC-LST-01 A1.
func TestReadFailures(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		opt     func(*env)
		failure domain.Failure
		updates int // start, then frames when they were saved, then the failure
		frames  int
	}{
		{"unreadable", func(e *env) { e.cut.err = aidomain.ErrUnreadable }, domain.FailUnreadable, 2, 0},
		{"refused by ai", func(e *env) { e.cut.err = aidomain.ErrRefused }, domain.FailUnavailable, 2, 0},
		{"too short", func(e *env) { e.cut.err = aidomain.ErrTooShort }, domain.FailTooShort, 2, 0},
		{"too long", func(e *env) { e.cut.err = aidomain.ErrTooLong }, domain.FailTooLong, 2, 0},
		{"no frames", func(e *env) { e.cut.frames = 0 }, domain.FailUnreadable, 2, 0},
		{"no item", func(e *env) { e.model.answer = noItemReply }, domain.FailNoItem, 3, 3},
		{"model refused", func(e *env) { e.model.err = fmt.Errorf("model answered 400: %w", llm.ErrRefused) }, domain.FailUnavailable, 3, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, c.opt)
			seller := accounttest.WithPhone(t, e.db)
			v := e.end(t, seller, e.start(t, seller).ID)
			if v.Status != domain.StatusFailed || v.Failure != c.failure || v.Draft != nil || len(v.Photos) != c.frames {
				t.Fatalf("%+v", v)
			}
			if n := e.updates(t, seller, v.ID); n != c.updates {
				t.Fatalf("%d updates, want %d", n, c.updates)
			}
			if s := e.videoOf(t, v.ID); s != mediadomain.StatusConsumed {
				t.Fatalf("video %s", s)
			}
		})
	}
}

// A video with no sound track still gives a draft, from the frames alone.
func TestReadWithoutSound(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(e *env) { e.stt.err = aidomain.ErrUnreadable })
	seller := accounttest.WithPhone(t, e.db)
	v := e.end(t, seller, e.start(t, seller).ID)
	if v.Status != domain.StatusDone || v.Draft == nil {
		t.Fatalf("%+v", v)
	}
	if _, _, user := e.model.asked(); strings.Contains(user, transcript) {
		t.Fatalf("the model heard %q", user)
	}
}

// A run that reaches a step after the deadline fails the read with
// unavailable and asks no model.
func TestReadPastTheDeadline(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(x *env) { x.cut.onCall = func() { x.clock.Add(3 * time.Minute) } })
	seller := accounttest.WithPhone(t, e.db)
	v := e.end(t, seller, e.start(t, seller).ID)
	if v.Status != domain.StatusFailed || v.Failure != domain.FailUnavailable || len(v.Photos) != 3 {
		t.Fatalf("%+v", v)
	}
	if calls, _, _ := e.model.asked(); calls != 0 {
		t.Fatalf("the model was asked %d times", calls)
	}
	if n := e.updates(t, seller, v.ID); n != 3 {
		t.Fatalf("%d updates, want start, frames and the failure", n)
	}
	if s := e.videoOf(t, v.ID); s != mediadomain.StatusConsumed {
		t.Fatalf("video %s", s)
	}
}

// A model that keeps failing is retried until the deadline, then the read
// fails with unavailable. The frames step leaves two seconds.
//
// UC-LST-01 A1.
func TestReadModelKeepsFailing(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(x *env) {
		x.cut.onCall = func() { x.clock.Add(domain.Deadline - 2*time.Second) }
		x.model.err = errors.New("the model is down")
	})
	seller := accounttest.WithPhone(t, e.db)
	v := e.end(t, seller, e.start(t, seller).ID)
	if v.Status != domain.StatusFailed || v.Failure != domain.FailUnavailable {
		t.Fatalf("%+v", v)
	}
	if calls, _, _ := e.model.asked(); calls < 2 {
		t.Fatalf("the model was asked %d times; Restate should retry", calls)
	}
	if s := e.videoOf(t, v.ID); s != mediadomain.StatusConsumed {
		t.Fatalf("video %s", s)
	}
}

// sampleOf is catalog whose price sample is samples; it keeps the query.
type sampleOf struct {
	catalogsvc.Service

	mu      sync.Mutex
	samples []catalogsvc.PriceSample
	asked   catalogsvc.PriceQuery
}

func (c *sampleOf) PriceSample(_ context.Context, q catalogsvc.PriceQuery) ([]catalogsvc.PriceSample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = q
	return c.samples, nil
}

// failingEmbed is an embedding model that is down.
type failingEmbed struct{}

func (failingEmbed) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("the embedding model is down")
}

// UC-LST-01 E4, E6.
func TestStartReadRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)

	reads := func(account uuid.UUID) int {
		var n int
		if err := e.db.QueryRow(ctx, `SELECT count(*) FROM listing_reads WHERE account_id = @a AND source = 'video'`,
			pgx.StrictNamedArgs{"a": account}).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	start := func(account, video uuid.UUID) error {
		_, err := e.assist.StartVideoRead(ctx, account, uuid.NewV7(), video, "")
		return err
	}

	if err := start(me, e.video(t, other)); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("someone else's video: %v", err)
	}
	photo := mediatest.Upload(t, e.media, me, "image/jpeg", mediatest.JPEG(t, 64, 48))
	if err := start(me, photo); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("a photo: %v", err)
	}
	if n := reads(me); n != 0 {
		t.Fatalf("%d reads left by refused starts", n)
	}
	video := e.video(t, me)
	if err := start(me, video); err != nil {
		t.Fatal(err)
	}
	if err := start(me, video); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("a video read twice: %v", err)
	}

	busy := accounttest.WithPhone(t, e.db)
	for range domain.VideoCap {
		if _, err := e.reads.Insert(ctx, domain.NewVideoRead(uuid.NewV7(), busy, uuid.NewV7(), e.clock.Now()), uuid.Nil()); err != nil {
			t.Fatal(err)
		}
	}
	if err := start(busy, e.video(t, busy)); !errors.Is(err, domain.ErrReadLimit) {
		t.Fatalf("at the cap: %v", err)
	}

	noPhone := accounttest.New(t, e.db)
	if err := start(noPhone, e.video(t, noPhone)); !errors.Is(err, accountdomain.ErrPhoneRequired) {
		t.Fatalf("no phone: %v", err)
	}

	suspended := accounttest.WithPhone(t, e.db)
	v := e.video(t, suspended)
	accounttest.Suspend(t, e.accounts, suspended)
	if err := start(suspended, v); err == nil || apperr.From(err).Code != "account_suspended" {
		t.Fatalf("suspended: %v", err)
	}
}

// One video sent twice at once under two keys gives one read: the other
// start is invalid_upload and leaves no row behind to count toward the cap.
func TestStartReadRace(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.WithPhone(t, e.db)
	video := e.video(t, me)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := e.assist.StartVideoRead(ctx, me, uuid.NewV7(), video, "")
			errs <- err
		}()
	}
	var ok, refused int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			ok++
		case errors.Is(err, mediadomain.ErrInvalidUpload):
			refused++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("%d started, %d refused", ok, refused)
	}
	var n int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM listing_reads WHERE account_id = @a AND source = 'video'`,
		pgx.StrictNamedArgs{"a": me}).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d reads, %v", n, err)
	}
}

// A repeated Idempotency-Key gets the first read and starts no second one.
func TestStartReadTwice(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.WithPhone(t, e.db)
	key, video := uuid.NewV7(), e.video(t, me)
	first, err := e.assist.StartVideoRead(ctx, me, key, video, "")
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.assist.StartVideoRead(ctx, me, key, video, "")
	if err != nil || again.ID != first.ID {
		t.Fatalf("again: %+v %v", again, err)
	}
	var n int
	if err = e.db.QueryRow(ctx, `SELECT count(*) FROM listing_reads WHERE account_id = @a AND source = 'video'`,
		pgx.StrictNamedArgs{"a": me}).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d reads, %v", n, err)
	}
}

// Only the reader reads a read, and only before it expires.
func TestGetRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	v := e.end(t, me, e.start(t, me).ID)
	if _, err := e.assist.GetRead(ctx, other, v.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another account: %v", err)
	}
	if _, err := e.assist.GetRead(ctx, me, uuid.NewV7()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	e.clock.Add(domain.KeepFor + time.Minute)
	if _, err := e.assist.GetRead(ctx, me, v.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired: %v", err)
	}
}

// Expire deletes the read and consumes its video; a second call changes
// nothing.
func TestExpireDeletesTheRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.WithPhone(t, e.db)
	v := e.end(t, me, e.start(t, me).ID)
	status := e.videoOf(t, v.ID)
	r, err := e.reads.Get(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := service.ExpireInput{Read: v.ID, Video: r.Media()}
	for range 2 {
		if _, err = ingress.Service[service.ExpireInput, restate.Void](e.ingress, e.prefix+"VideoRead", "Expire").
			Request(ctx, in, restate.WithIdempotencyKey(uuid.NewV7().String())); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.reads.Get(ctx, v.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("read still there: %v", err)
	}
	if status != mediadomain.StatusConsumed {
		t.Fatalf("video %s", status)
	}
}

func TestDeleteAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.WithPhone(t, e.db)
	v := e.end(t, me, e.start(t, me).ID)
	for range 2 {
		if err := e.assist.DeleteAccount(ctx, me); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.reads.Get(ctx, v.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("read still there: %v", err)
	}
}

// A refusal from speech to text (a bad key, a spent budget, an error code
// the gateway rewrote) is not the seller's video: the frames step already
// read it, so the read goes on without the speech.
func TestReadWhenSpeechToTextRefuses(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(x *env) { x.stt.err = aidomain.ErrRefused })
	seller := accounttest.WithPhone(t, e.db)
	v := e.end(t, seller, e.start(t, seller).ID)
	if v.Status != domain.StatusDone || v.Draft == nil {
		t.Fatalf("%+v", v)
	}
	if _, _, user := e.model.asked(); strings.Contains(user, transcript) {
		t.Fatalf("the model got a transcript: %q", user)
	}
}

// A fill step that keeps failing stops after three attempts, so an outage
// of the model does not transcribe one video and ask the model again every
// two seconds until the deadline.
func TestReadFillTriesThreeTimes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(x *env) { x.model.err = errors.New("the model is down") })
	seller := accounttest.WithPhone(t, e.db)
	v := e.end(t, seller, e.start(t, seller).ID)
	if v.Status != domain.StatusFailed || v.Failure != domain.FailUnavailable {
		t.Fatalf("%+v", v)
	}
	if calls, _, _ := e.model.asked(); calls != 3 {
		t.Fatalf("the model was asked %d times, want 3", calls)
	}
}

// flakyHold loses the hold's first save to a race, as a second start of the
// same video does when its hold overlaps this one.
type flakyHold struct {
	mediasvc.Service

	mu    sync.Mutex
	calls int
}

func (f *flakyHold) HoldVideo(ctx context.Context, owner, holder, id, writeID uuid.UUID) error {
	f.mu.Lock()
	f.calls++
	first := f.calls == 1
	f.mu.Unlock()
	if first {
		return apperr.ErrTryAgain
	}
	return f.Service.HoldVideo(ctx, owner, holder, id, writeID)
}

// A hold that lost a race runs again: the start answers the read, never
// try_again, which Restate would keep under the Idempotency-Key.
func TestStartReadRetriesALostHold(t *testing.T) {
	t.Parallel()
	hold := &flakyHold{}
	e := newEnv(t, func(x *env) { hold.Service = x.media; x.deps.Media = hold })
	me := accounttest.WithPhone(t, e.db)
	if _, err := e.assist.StartVideoRead(context.Background(), me, uuid.NewV7(), e.video(t, me), ""); err != nil {
		t.Fatalf("start: %v", err)
	}
}

// Expire consumes the video even when the read's row is gone, as after the
// account's deletion between Start and Run.
func TestExpireConsumesTheVideoOfAGoneRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, read := accounttest.WithPhone(t, e.db), uuid.NewV7()
	video := e.video(t, me)
	if err := e.media.HoldVideo(ctx, me, read, video, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if _, err := ingress.Service[service.ExpireInput, restate.Void](e.ingress, e.prefix+"VideoRead", "Expire").
		Request(ctx, service.ExpireInput{Read: read, Video: video}, restate.WithIdempotencyKey(uuid.NewV7().String())); err != nil {
		t.Fatal(err)
	}
	u, err := mediarepo.NewUploads(e.db).Get(ctx, video)
	if err != nil || u.Status() != mediadomain.StatusConsumed {
		t.Fatalf("video %s %v", u.Status(), err)
	}
}

// The phone's transcript reaches the model, no speech to text runs, and
// nothing of it is left when the read ends.
func TestReadWithThePhonesTranscript(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller := accounttest.WithPhone(t, e.db)
	heard := "Xe đạp Asama còn mới, mình bán một triệu hai, có thương lượng."
	v := e.end(t, seller, e.startHeard(t, seller, uuid.NewV7(), heard).ID)
	if v.Status != domain.StatusDone {
		t.Fatalf("%+v", v)
	}
	if n := e.stt.called(); n != 0 {
		t.Fatalf("speech to text ran %d times", n)
	}
	_, _, user := e.model.asked()
	if !strings.Contains(user, heard) || !strings.Contains(user, "from the seller's phone") {
		t.Fatalf("the model was asked %q", user)
	}
	var left int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE transcript IS NOT NULL) +
       (SELECT count(*) FROM listing_read_transcripts WHERE account_id = @a) FROM listing_reads WHERE account_id = @a`,
		pgx.StrictNamedArgs{"a": seller}).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d transcripts left: %v", left, err)
	}
}

// A transcript of spaces reads the video's own sound, as none does.
func TestBlankTranscriptReadsTheSound(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller := accounttest.WithPhone(t, e.db)
	v := e.end(t, seller, e.startHeard(t, seller, uuid.NewV7(), " \n\t ").ID)
	_, _, user := e.model.asked()
	if v.Status != domain.StatusDone || e.stt.called() != 1 || !strings.Contains(user, transcript) {
		t.Fatalf("%+v, stt %d, asked %q", v, e.stt.called(), user)
	}
}

// A repeated key answers the first read, and the first transcript is the
// one read.
func TestRepeatedKeyKeepsTheFirstTranscript(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller := accounttest.WithPhone(t, e.db)
	key := uuid.NewV7()
	video := e.video(t, seller)
	first, err := e.assist.StartVideoRead(context.Background(), seller, key, video, "lần đầu: một triệu hai")
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.assist.StartVideoRead(context.Background(), seller, key, video, "lần sau")
	if err != nil || again.ID != first.ID {
		t.Fatalf("%v %v", again.ID, err)
	}
	e.end(t, seller, first.ID)
	if _, _, user := e.model.asked(); !strings.Contains(user, "lần đầu") || strings.Contains(user, "lần sau") {
		t.Fatalf("the model was asked %q", user)
	}
}

// waitingTranscripts counts the transcripts still waiting for account's reads.
func waitingTranscripts(t *testing.T, e env, account uuid.UUID) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM listing_read_transcripts WHERE account_id = @a`,
		pgx.StrictNamedArgs{"a": account}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A retry's transcript, and the transcript of a refused start, do not wait
// for a read that never takes them.
func TestNoTranscriptWaitsAfterTheStart(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := accounttest.WithPhone(t, e.db)
	key, video := uuid.NewV7(), e.video(t, seller)
	if _, err := e.assist.StartVideoRead(ctx, seller, key, video, "lần đầu"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.assist.StartVideoRead(ctx, seller, key, video, "lần sau"); err != nil {
		t.Fatal(err)
	}
	if n := waitingTranscripts(t, e, seller); n != 0 {
		t.Fatalf("%d transcripts wait after a retry", n)
	}

	busy := accounttest.WithPhone(t, e.db)
	for range domain.VideoCap {
		if _, err := e.reads.Insert(ctx, domain.NewVideoRead(uuid.NewV7(), busy, uuid.NewV7(), e.clock.Now()), uuid.Nil()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.assist.StartVideoRead(ctx, busy, uuid.NewV7(), e.video(t, busy), "một triệu hai"); !errors.Is(err, domain.ErrReadLimit) {
		t.Fatalf("a read past the cap: %v", err)
	}
	if n := waitingTranscripts(t, e, busy); n != 0 {
		t.Fatalf("%d transcripts wait after a refused start", n)
	}
}
