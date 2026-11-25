package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	mediarepo "github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/provider/llm"
)

// A photo read fills a draft from one image, keeps the photo as the
// draft's only photo, and leaves the photo waiting for the listing.
//
// UC-LST-01.
func TestPhotoReadFillsADraft(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller := accounttest.WithPhone(t, e.db)
	photo := e.photo(t, seller)
	started := e.startPhoto(t, seller, photo)
	if started.Status != domain.StatusReading || started.Source != domain.SourcePhoto || started.Draft != nil {
		t.Fatalf("started: %+v", started)
	}
	v := e.end(t, seller, started.ID)
	if v.Status != domain.StatusDone || len(v.Photos) != 1 || v.Photos[0].UploadID != photo || v.Photos[0].URL == "" ||
		v.Photos[0].AtSeconds != nil || v.Link != nil || v.ContactRemoved != nil {
		t.Fatalf("%+v", v)
	}
	d := v.Draft
	if d == nil || d.Title != "iPhone 11 Trắng" || d.CategoryCode != "phones.iphone" || d.Condition != catalogdomain.ConditionUsed ||
		d.Price != nil || d.AllowOffers || len(d.Specs) != 0 || len(d.Photos) != 1 || d.Photos[0] != photo {
		t.Fatalf("draft %+v", d)
	}
	calls, images, user := e.model.asked()
	if calls != 1 || images != 1 || user != "The seller's photo is attached below.\n" || e.stt.called() != 0 {
		t.Fatalf("model asked %d times, %d images, %q; stt %d", calls, images, user, e.stt.called())
	}
	if n := e.updates(t, seller, v.ID); n != 2 {
		t.Fatalf("%d updates, want start and done", n)
	}
	u, err := mediarepo.NewUploads(e.db).Get(context.Background(), photo)
	if err != nil || u.Status() != mediadomain.StatusWaiting {
		t.Fatalf("the photo is %s: %v", u.Status(), err)
	}
}

// UC-LST-01 A1.
func TestPhotoReadFailures(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		opt     func(*env)
		failure domain.Failure
		reason  string
		calls   int
	}{
		// The model is asked again once when it sees nothing.
		{"no item", func(e *env) { e.model.photo = noItemReply }, domain.FailNoItem, "Ảnh chỉ có một người.", 2},
		{
			"model refused", func(e *env) { e.model.err = &llm.APIError{Status: 400, Message: "image too large"} },
			domain.FailUnavailable, "AI báo lỗi 400: image too large", 1,
		},
		// A 5xx is retried; the reason survives Restate's retries as text.
		{
			"model down", func(e *env) { e.model.err = &llm.APIError{Status: 503, Message: "overloaded"} },
			domain.FailUnavailable, "AI báo lỗi 503: overloaded", 3,
		},
		{"not JSON", func(e *env) { e.model.photo = "iPhone 11 Trắng" }, domain.FailUnavailable, "AI trả lời sai định dạng. Thử lại nhé.", 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, c.opt)
			seller := accounttest.WithPhone(t, e.db)
			v := e.end(t, seller, e.startPhoto(t, seller, e.photo(t, seller)).ID)
			if v.Status != domain.StatusFailed || v.Failure != c.failure || v.FailureReason != c.reason || v.Draft != nil || v.Photos != nil {
				t.Fatalf("%+v", v)
			}
			if calls, _, _ := e.model.asked(); calls != c.calls {
				t.Fatalf("model asked %d times, want %d", calls, c.calls)
			}
		})
	}
}

// A photo posted (or gone) between the start and the fill ends the read
// as unreadable at once, without the model. The read is inserted as Start
// leaves it, the photo attached, then Run is called directly.
func TestPhotoReadOfAPostedPhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := accounttest.WithPhone(t, e.db)
	photo := e.photo(t, seller)
	r := domain.NewPhotoRead(uuid.NewV7(), seller, photo, e.clock.Now())
	if _, err := e.reads.Insert(ctx, r, uuid.Nil()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `UPDATE uploads SET status = 'attached', holder_id = gen_random_uuid(), shape = 'photo' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": photo}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingress.Service[uuid.UUID, restate.Void](e.ingress, e.prefix+"PhotoRead", "Run").
		Request(ctx, r.ID(), restate.WithIdempotencyKey(uuid.NewV7().String())); err != nil {
		t.Fatal(err)
	}
	v := e.end(t, seller, r.ID())
	if calls, _, _ := e.model.asked(); v.Status != domain.StatusFailed || v.Failure != domain.FailUnreadable || calls != 0 {
		t.Fatalf("%+v, model asked %d times", v, calls)
	}
}

func TestPhotoReadRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller := accounttest.WithPhone(t, e.db)
	other := accounttest.WithPhone(t, e.db)
	for name, photo := range map[string]uuid.UUID{
		"someone else's photo": e.photo(t, other),
		"a video":              e.video(t, seller),
		"no such upload":       uuid.NewV7(),
	} {
		if _, err := e.assist.StartPhotoRead(context.Background(), seller, uuid.NewV7(), photo); !errors.Is(err, mediadomain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM listing_reads WHERE account_id = @a`,
		pgx.StrictNamedArgs{"a": seller}).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d refused reads kept: %v", n, err)
	}
}

// A guess that fails leaves the draft without a category; the read still
// fills the rest and the seller picks the category.
func TestPhotoReadWithoutAGuess(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.catText.SetFail(errors.New("embedding down"))
	seller := accounttest.WithPhone(t, e.db)
	v := e.end(t, seller, e.startPhoto(t, seller, e.photo(t, seller)).ID)
	if v.Status != domain.StatusDone || v.Draft == nil || v.Draft.Title != "iPhone 11 Trắng" || v.Draft.CategoryCode != "" {
		t.Fatalf("%+v %+v", v, v.Draft)
	}
}
