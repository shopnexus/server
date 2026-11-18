package service_test

import (
	"context"
	"errors"
	"fmt"
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
		d.Price != nil || d.AllowOffers || len(d.Photos) != 1 || d.Photos[0] != photo {
		t.Fatalf("draft %+v", d)
	}
	if v.Estimate == nil || *v.Estimate == nil || (*v.Estimate).Source != domain.SourceModel || (*v.Estimate).Median != 4_500_000 {
		t.Fatalf("estimate %+v", v.Estimate)
	}
	calls, images, user := e.model.asked()
	if calls != 1 || images != 1 || user != "The seller's photo is attached below.\n" || e.stt.called() != 0 {
		t.Fatalf("model asked %d times, %d images, %q; stt %d", calls, images, user, e.stt.called())
	}
	if n := e.updates(t, seller, v.ID); n != 3 {
		t.Fatalf("%d updates, want start, draft and done", n)
	}
	u, err := mediarepo.NewUploads(e.db).Get(context.Background(), photo)
	if err != nil || u.Status() != mediadomain.StatusWaiting {
		t.Fatalf("the photo is %s: %v", u.Status(), err)
	}
}

// UC-LST-01 A1.
func TestPhotoReadFailures(t *testing.T) {
	t.Parallel()
	noItem := iphonePhoto()
	noItem.IsItem = false
	for _, c := range []struct {
		name    string
		opt     func(*env)
		failure domain.Failure
	}{
		{"no item", func(e *env) { e.model.photo = noItem }, domain.FailNoItem},
		{"model refused", func(e *env) { e.model.err = fmt.Errorf("model answered 400: %w", llm.ErrRefused) }, domain.FailUnavailable},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, c.opt)
			seller := accounttest.WithPhone(t, e.db)
			v := e.end(t, seller, e.startPhoto(t, seller, e.photo(t, seller)).ID)
			if v.Status != domain.StatusFailed || v.Failure != c.failure || v.Draft != nil || v.Photos != nil {
				t.Fatalf("%+v", v)
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
	if err := e.reads.Insert(ctx, r, ""); err != nil {
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
		if _, err := e.assist.StartPhotoRead(context.Background(), seller, uuid.NewV7().String(), photo); !errors.Is(err, mediadomain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM listing_reads WHERE account_id = @a`,
		pgx.StrictNamedArgs{"a": seller}).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d refused reads kept: %v", n, err)
	}
}
