package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/provider/chotot"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// A link read goes through read, copy and estimate, notifies after each
// step, copies the photos as the seller's, and takes the phone out.
func TestLinkReadFillsADraft(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller := accounttest.WithPhone(t, e.db)
	started := e.startLink(t, seller)
	if started.Status != domain.StatusReading || started.Source.URL != "https://www.chotot.com/119482736.htm" ||
		started.Source.Site != "chotot.com" || started.Draft != nil {
		t.Fatalf("started: %+v", started)
	}
	v := e.linkEnd(t, seller, started.ID)
	if v.Status != domain.StatusDone || v.Failure != "" || v.Source.PhotoCount != 5 || len(v.Photos) != 5 {
		t.Fatalf("%+v", v)
	}
	d := v.Draft
	if d == nil || d.Title != "iPhone 11 64GB Trắng, pin 81%" || d.CategoryCode != "phones.iphone" ||
		d.Condition != catalogdomain.ConditionUsed || d.Price == nil || *d.Price != 4_500_000 || !d.AllowOffers ||
		len(d.Photos) != 5 || strings.Contains(d.Description, "0909") {
		t.Fatalf("draft %+v", d)
	}
	if v.ContactRemoved == nil || !*v.ContactRemoved {
		t.Fatal("the phone was taken out, so contact_removed")
	}
	for i, p := range v.Photos {
		if p.UploadID != d.Photos[i] || p.URL == "" {
			t.Fatalf("photo %d: %+v", i, p)
		}
		u, err := mediarepo.NewUploads(e.db).Get(context.Background(), p.UploadID)
		if err != nil || u.OwnerID() != seller || u.Origin() != mediadomain.OriginLink || u.Status() != mediadomain.StatusWaiting {
			t.Fatalf("upload %d: %+v %v", i, u.State(), err)
		}
	}
	if v.Estimate == nil || *v.Estimate == nil || (*v.Estimate).Source != domain.SourceModel || (*v.Estimate).Median != 4_500_000 {
		t.Fatalf("estimate %+v", v.Estimate)
	}
	_, images, user := e.model.asked()
	if images != 0 || !strings.Contains(user, "iPhone 11 64GB trắng pin 81% zin all") || !strings.Contains(user, "Dung lượng: 64 GB") ||
		!strings.Contains(user, "Điện thoại") {
		t.Fatalf("model asked with %d images, %q", images, user)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM account_updates
WHERE account_id = @account AND kind = 'link_read' AND link_read_id = @read`,
		pgx.StrictNamedArgs{"account": seller, "read": v.ID}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("%d updates, want start, read, copy and done", n)
	}
}

// Each way a link read ends early is a failure on the read.
func TestLinkReadFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(*env)
		want  domain.Failure
		calls int // model calls, -1 to skip the check
	}{
		{"no such listing", func(e *env) { e.source.adErr = chotot.ErrNotFound }, domain.FailListingNotFound, 0},
		{"Chợ Tốt changed", func(e *env) { e.source.adErr = chotot.ErrFormat }, domain.FailUnavailable, 0},
		{"Chợ Tốt down", func(e *env) { e.source.adErr = chotot.ErrUnavailable }, domain.FailUnavailable, 0},
		{"the model refused", func(e *env) { e.model.err = llm.ErrRefused }, domain.FailUnavailable, 1},
		{"not an item", func(e *env) { e.model.link.IsItem = false }, domain.FailNoItem, 1},
		{"no photo copied", func(e *env) { e.source.images = map[string][]byte{} }, domain.FailListingNotFound, -1},
		{"the CDN down", func(e *env) { e.source.imageErr = chotot.ErrUnavailable }, domain.FailUnavailable, -1},
		{"photos moved off the CDN", func(e *env) { e.source.imageErr = chotot.ErrOffCDN }, domain.FailUnavailable, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, tc.setup)
			seller := accounttest.WithPhone(t, e.db)
			v := e.linkEnd(t, seller, e.startLink(t, seller).ID)
			if v.Status != domain.StatusFailed || v.Failure != tc.want || v.Draft != nil || v.Estimate != nil {
				t.Fatalf("%+v", v)
			}
			if calls, _, _ := e.model.asked(); tc.calls >= 0 && calls != tc.calls {
				t.Fatalf("%d model calls, want %d", calls, tc.calls)
			}
			e.source.mu.Lock()
			adCalls := e.source.adCalls
			e.source.mu.Unlock()
			if tc.name == "Chợ Tốt down" && adCalls != 3 {
				t.Fatalf("%d reads of the listing, want 3 attempts", adCalls)
			}
		})
	}
	// The read step takes three minutes; the copy step then starts past the
	// deadline.
	t.Run("past the deadline", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t, func(x *env) { x.source.onAd = func() { x.clock.Add(3 * time.Minute) } })
		seller := accounttest.WithPhone(t, e.db)
		v := e.linkEnd(t, seller, e.startLink(t, seller).ID)
		if v.Status != domain.StatusFailed || v.Failure != domain.FailUnavailable || v.Source.PhotoCount != 5 || v.Draft != nil {
			t.Fatalf("%+v", v)
		}
	})
}

// A photo the CDN will not give is skipped; the others keep their order.
func TestLinkReadSkipsABadPhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(x *env) { delete(x.source.images, photoURL(1)) })
	seller := accounttest.WithPhone(t, e.db)
	v := e.linkEnd(t, seller, e.startLink(t, seller).ID)
	if v.Status != domain.StatusDone || v.Source.PhotoCount != 5 || len(v.Photos) != 4 || len(v.Draft.Photos) != 4 {
		t.Fatalf("%+v", v)
	}
}

// A photo the CDN keeps failing is skipped once the step has asked again;
// the others are copied.
func TestLinkReadSkipsAPhotoTheCDNKeepsFailing(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(x *env) { x.source.imageErrs = map[string]error{photoURL(2): chotot.ErrUnavailable} })
	seller := accounttest.WithPhone(t, e.db)
	v := e.linkEnd(t, seller, e.startLink(t, seller).ID)
	if v.Status != domain.StatusDone || len(v.Photos) != 4 {
		t.Fatalf("%+v", v)
	}
}

func TestStartLinkReadRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	start := func(account uuid.UUID, link string) error {
		_, err := e.assist.StartLinkRead(ctx, account, uuid.NewV7().String(), link)
		return err
	}
	me := accounttest.WithPhone(t, e.db)
	if err := start(me, "iphone 11 trắng"); !errors.Is(err, domain.ErrNotALink) {
		t.Fatalf("not a link: %v", err)
	}
	if err := start(me, "https://www.facebook.com/groups/passdophcm/posts/2048817356"); !errors.Is(err, domain.ErrUnsupportedSource) {
		t.Fatalf("facebook: %v", err)
	}
	key := uuid.NewV7().String()
	first, err := e.assist.StartLinkRead(ctx, me, key, iphoneLink)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.assist.StartLinkRead(ctx, me, key, iphoneLink)
	if err != nil || again.ID != first.ID {
		t.Fatalf("a repeated key: %s after %s, %v", again.ID, first.ID, err)
	}
	busy := accounttest.WithPhone(t, e.db)
	for range domain.ReadLimit {
		link, _ := domain.ParseLink(iphoneLink)
		if err = e.links.Insert(ctx, domain.NewOldLinkRead(uuid.NewV7(), busy, link, e.clock.Now())); err != nil {
			t.Fatal(err)
		}
	}
	if err = start(busy, iphoneLink); !errors.Is(err, domain.ErrReadLimit) {
		t.Fatalf("thirty reads: %v", err)
	}
	if err = start(accounttest.New(t, e.db), iphoneLink); !errors.Is(err, accountdomain.ErrPhoneRequired) {
		t.Fatalf("no phone: %v", err)
	}
	suspended := accounttest.WithPhone(t, e.db)
	accounttest.Suspend(t, e.accounts, suspended)
	if err = start(suspended, iphoneLink); err == nil || apperr.From(err).Code != "account_suspended" {
		t.Fatalf("suspended: %v", err)
	}
}

func TestGetLinkRead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	v := e.linkEnd(t, me, e.startLink(t, me).ID)
	if _, err := e.assist.GetLinkRead(ctx, other, v.ID); !errors.Is(err, domain.ErrLinkReadNotFound) {
		t.Fatalf("another account: %v", err)
	}
	e.clock.Add(domain.KeepFor)
	if _, err := e.assist.GetLinkRead(ctx, me, v.ID); !errors.Is(err, domain.ErrLinkReadNotFound) {
		t.Fatalf("expired: %v", err)
	}
}

func TestPreviewLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db) // a preview needs no phone
	p, err := e.assist.PreviewLink(ctx, me, iphoneLink)
	if err != nil || p.Site != "chotot.com" || p.URL != "https://www.chotot.com/119482736.htm" || p.Title != "iPhone 11 64GB trắng pin 81% zin all" ||
		p.Price == nil || *p.Price != 4_500_000 || p.CoverURL != photoURL(0) || p.PhotoCount != 5 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err = e.assist.PreviewLink(ctx, me, "iphone"); !errors.Is(err, domain.ErrNotALink) {
		t.Fatalf("not a link: %v", err)
	}
	e.source.mu.Lock()
	e.source.adErr = chotot.ErrNotFound
	e.source.mu.Unlock()
	if _, err = e.assist.PreviewLink(ctx, me, iphoneLink); !errors.Is(err, domain.ErrListingNotFound) {
		t.Fatalf("no such listing: %v", err)
	}
	for _, cause := range []error{chotot.ErrUnavailable, chotot.ErrFormat} {
		e.source.mu.Lock()
		e.source.adErr = cause
		e.source.mu.Unlock()
		if _, err = e.assist.PreviewLink(ctx, me, iphoneLink); !errors.Is(err, domain.ErrSourceUnavailable) {
			t.Fatalf("%v: %v", cause, err)
		}
	}
	suspended := accounttest.New(t, e.db)
	accounttest.Suspend(t, e.accounts, suspended)
	if _, err = e.assist.PreviewLink(ctx, suspended, iphoneLink); err == nil || apperr.From(err).Code != "account_suspended" {
		t.Fatalf("suspended: %v", err)
	}
}

// A deleted account's link reads go with its video reads.
func TestDeleteAccountDeletesLinkReads(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	me := accounttest.WithPhone(t, e.db)
	v := e.linkEnd(t, me, e.startLink(t, me).ID)
	if err := e.assist.DeleteAccount(context.Background(), me); err != nil {
		t.Fatal(err)
	}
	if _, err := e.links.Get(context.Background(), v.ID); !errors.Is(err, domain.ErrLinkReadNotFound) {
		t.Fatalf("%v", err)
	}
}
