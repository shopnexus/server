package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/chotot/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/testenv"
)

// UC-LST-04 A2, A6, E1, E2, E3, E4, E5, E6.
func TestLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := e.seller(t), e.seller(t)
	profile := oid()
	if _, err := e.svc.Link(ctx, me, "https://www.chotot.com/"+profile); !isCode(err, domain.ErrInvalidProfileURL) {
		t.Fatalf("a listing's address: %v", err)
	}
	if _, err := e.svc.Link(ctx, me, profileURL(profile)); !isCode(err, domain.ErrCodeExpired) {
		t.Fatalf("no code: %v", err)
	}
	code, err := e.svc.Code(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := e.svc.Code(ctx, me); again.Code != code.Code {
		t.Fatalf("a second code while the first is live: %v", again)
	}
	if _, err = e.svc.Link(ctx, me, profileURL(profile)); !isCode(err, domain.ErrProfileNotFound) {
		t.Fatalf("no profile: %v", err)
	}
	e.source.setBio(profile, "Chưa có mã")
	_, err = e.svc.Link(ctx, me, profileURL(profile))
	if refused := apperr.From(err); !isCode(err, domain.ErrCodeNotFound) || refused.Details.Profile == nil ||
		refused.Details.Profile.Name != "Shop Na" || refused.Details.Profile.AvatarURL != nil {
		t.Fatalf("no code in the bio: %v %+v", err, refused.Details)
	}
	e.source.setDown(true)
	if _, err = e.svc.Link(ctx, me, profileURL(profile)); !isCode(err, domain.ErrUnavailable) {
		t.Fatalf("down: %v", err)
	}
	e.source.setDown(false)
	old := e.source.add(profile, "Xe đạp Thống Nhất", 900_000, 2050)
	e.source.setBio(profile, "Mã: "+code.Code[:5]+" "+code.Code[5:])
	v, err := e.svc.Link(ctx, me, "chotot.com/user/"+profile+"?src=app")
	if err != nil || v.OID != profile || v.Name != "Shop Na" || !v.AutoImport || !v.FollowUpdates || !v.RemindHide ||
		v.Waiting == nil || *v.Waiting != 1 || v.CheckedAt != nil || v.Imported != 0 {
		t.Fatalf("link: %+v %v", v, err)
	}
	if len(e.checks.links) != 1 {
		t.Fatalf("checks %v", e.checks.links)
	}
	if _, err = e.svc.Link(ctx, me, profileURL(profile)); !isCode(err, domain.ErrCodeExpired) {
		t.Fatalf("the code was not used up: %v", err)
	}
	off := false
	if v, err = e.svc.Update(ctx, me, domain.SettingsPatch{RemindHide: &off}); err != nil || v.RemindHide || !v.AutoImport {
		t.Fatalf("update: %+v %v", v, err)
	}
	// Another account proves it holds the profile and takes it over.
	e.link(t, other, profile)
	if st, _ := e.svc.Get(ctx, me); st.Link != nil {
		t.Fatalf("the first account kept the link: %+v", st.Link)
	}
	if st, _ := e.svc.Get(ctx, other); st.Link == nil || st.Link.OID != profile {
		t.Fatalf("get: %+v", st)
	}
	if _, err = e.svc.Update(ctx, me, domain.SettingsPatch{RemindHide: &off}); !isCode(err, domain.ErrNotLinked) {
		t.Fatalf("update without a link: %v", err)
	}
	if _, err = e.svc.Ads(ctx, me); !isCode(err, domain.ErrNotLinked) {
		t.Fatalf("ads without a link: %v", err)
	}
	if err = e.svc.Unlink(ctx, other); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.svc.Get(ctx, other); st.Link != nil {
		t.Fatal("unlink")
	}
	_ = old
}

// waitSettled waits until account's import of listID is no longer
// importing and returns it.
func waitSettled(t *testing.T, e env, account uuid.UUID, listID int64) service.AdView {
	t.Helper()
	var got service.AdView
	testenv.Eventually(t, time.Minute, func() error {
		ads, err := e.svc.Ads(context.Background(), account)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(ads.Items, func(a service.AdView) bool { return a.ListID == listID })
		if i < 0 {
			t.Fatalf("listing %d is not listed", listID)
		}
		got = ads.Items[i]
		if got.State == domain.AdImporting {
			return errors.New("still importing")
		}
		return nil
	})
	return got
}

// UC-LST-04 A1, A5, E8.
func TestImport(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.seller(t)
	profile := oid()
	e.link(t, me, profile)
	bike := e.source.add(profile, "Xe đạp Giant ATX 2020", 3_500_000, 2050)
	noCat := e.source.add(profile, "Đồ nocat lạ", 200_000, 2050)
	flat := e.source.add(profile, "Căn hộ 2 phòng ngủ", 2_000_000_000, 1010)
	here := e.source.add(profile, "Xe đạp Asama", 1_500_000, 2050)
	repair := e.source.add(profile, "Nhận sửa xe service", 100_000, 2050)
	ads, err := e.svc.Ads(ctx, me)
	if err != nil || len(ads.Items) != 5 || ads.Room != catalogdomain.MaxOnSale-1 {
		t.Fatalf("ads: %+v %v", ads, err)
	}
	states := map[int64]domain.AdState{}
	for _, a := range ads.Items {
		states[a.ListID] = a.State
	}
	if states[bike] != domain.AdImportable || states[flat] != domain.AdUnsupported || states[here] != domain.AdAlreadyHere {
		t.Fatalf("states %v", states)
	}
	started, err := e.svc.Import(ctx, me, []int64{bike, noCat, repair})
	if err != nil || len(started.Items) != 3 || started.Items[0].State != domain.Importing {
		t.Fatalf("import: %+v %v", started, err)
	}
	got := waitSettled(t, e, me, bike)
	if got.State != domain.AdImported || got.ListingID == nil || got.Import == nil || got.Import.State != domain.Listed ||
		!slices.Equal(got.Import.Review, []string{"condition"}) {
		t.Fatalf("bike: %+v %+v", got, got.Import)
	}
	l, err := e.catalog.Get(ctx, &me, *got.ListingID, "vi")
	if err != nil || l.Title != "Xe đạp Giant ATX 2020" || l.Price != 3_500_000 || l.Condition != catalogdomain.ConditionUsed {
		t.Fatalf("the copy: %+v %v", l, err)
	}
	draft := waitSettled(t, e, me, noCat)
	if draft.State != domain.AdImported || draft.ListingID != nil || draft.Import.State != domain.Drafted ||
		!slices.Contains(draft.Import.Review, "category_code") || draft.Import.Draft == nil || len(draft.Import.Draft.Photos) != 3 {
		t.Fatalf("draft: %+v %+v", draft, draft.Import)
	}
	if s := waitSettled(t, e, me, repair); s.State != domain.AdUnsupported || s.Reason == nil || *s.Reason != domain.ReasonNoItem {
		t.Fatalf("repair: %+v", s)
	}
	// Importing again answers each as it is.
	again, err := e.svc.Import(ctx, me, []int64{bike})
	if err != nil || again.Items[0].State != domain.Listed || *again.Items[0].ListingID != *got.ListingID {
		t.Fatalf("again: %+v %v", again, err)
	}
	if st, _ := e.svc.Get(ctx, me); st.Link.Imported != 2 {
		t.Fatalf("imported %d", st.Link.Imported)
	}
	// The seller fills the draft's category and posts it.
	dr := draft.Import.Draft
	in := catalogdomain.ListingInput{
		Title: dr.Title, Price: *dr.Price, Description: dr.Description, CategoryCode: "furniture.other", Condition: dr.Condition,
		AllowOffers: true, CommuneCode: "00004", Specs: dr.Specs,
	}
	for _, p := range dr.Photos {
		in.Photos = append(in.Photos, p.UploadID)
	}
	if _, err = e.svc.PostDraft(ctx, me, bike, in); !isCode(err, domain.ErrDraftNotFound) && err != nil {
		t.Fatalf("a listed import: %v", err)
	}
	posted, err := e.svc.PostDraft(ctx, me, noCat, in)
	if err != nil || posted.State != domain.Listed || posted.ListingID == nil || len(posted.Review) != 0 {
		t.Fatalf("post draft: %+v %v", posted, err)
	}
	if _, err = e.svc.PostDraft(ctx, me, flat, in); !isCode(err, domain.ErrDraftNotFound) {
		t.Fatalf("no draft: %v", err)
	}
	// An edit of the copy clears its review; a deleted copy frees the listing.
	if err = e.svc.ListingEdited(ctx, *got.ListingID); err != nil {
		t.Fatal(err)
	}
	if a := waitSettled(t, e, me, bike); len(a.Import.Review) != 0 {
		t.Fatalf("review %v", a.Import.Review)
	}
	if err = e.svc.ListingDeleted(ctx, *got.ListingID); err != nil {
		t.Fatal(err)
	}
	if a := waitSettled(t, e, me, bike); a.Import != nil {
		t.Fatalf("a deleted copy's import stayed: %+v", a.Import)
	}
}

// UC-LST-04 E7.
func TestImportNotYours(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	me := e.seller(t)
	profile := oid()
	e.link(t, me, profile)
	theirs := e.source.add(oid(), "Xe đạp người khác", 1_000_000, 2050)
	if _, err := e.svc.Import(context.Background(), me, []int64{theirs}); err != nil {
		t.Fatal(err)
	}
	var imp domain.Import
	testenv.Eventually(t, time.Minute, func() error {
		var err error
		if err = e.db.QueryRow(context.Background(), `SELECT import_state, coalesce(reason, '') FROM chotot_imports
WHERE account_id = @a AND list_id = @l`, pgx.StrictNamedArgs{"a": me, "l": theirs}).Scan(&imp.State, &imp.Reason); err != nil {
			t.Fatal(err)
		}
		if imp.State == domain.Importing {
			return errors.New("still importing")
		}
		return nil
	})
	if imp.State != domain.Skipped || imp.Reason != domain.ReasonNotYours {
		t.Fatalf("%+v", imp)
	}
}

// UC-LST-04 A3, A4.
func TestSync(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := e.seller(t)
	profile := oid()
	old := e.source.add(profile, "Xe đạp cũ trước khi liên kết", 800_000, 2050)
	e.link(t, me, profile)
	kept := e.source.add(profile, "Xe đạp Giant mới đăng", 3_000_000, 2050)
	gone := e.source.add(profile, "Xe đạp Trinx mới đăng", 2_000_000, 2050)
	e.sync(t, me)
	ads, err := e.svc.Ads(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	listings := map[int64]*uuid.UUID{}
	for _, a := range ads.Items {
		listings[a.ListID] = a.ListingID
		if a.ListID == old && a.State != domain.AdImportable {
			t.Fatalf("a listing from before the link was imported: %+v", a)
		}
	}
	if listings[kept] == nil || listings[gone] == nil {
		t.Fatalf("the new listings were not imported: %+v", ads.Items)
	}
	if n := e.count(t, `SELECT count(*) FROM events e JOIN chotot_links l ON l.id = e.subject_id
WHERE e.kind = 'chotot_link.imported' AND l.account_id = @a AND (e.payload->>'count')::int = 2`, pgx.StrictNamedArgs{"a": me}); n != 1 {
		t.Fatalf("%d import events", n)
	}
	if st, _ := e.svc.Get(ctx, me); st.Link.CheckedAt == nil || *st.Link.Waiting != 1 || st.Link.Imported != 2 {
		t.Fatalf("checked: %+v", st.Link)
	}
	// Chợ Tốt reprices one and takes the other down.
	e.source.setPrice(kept, 2_800_000)
	e.source.remove(gone)
	e.sync(t, me)
	if l, _ := e.catalog.Get(ctx, &me, *listings[kept], "vi"); l.Price != 2_800_000 {
		t.Fatalf("not repriced: %d", l.Price)
	}
	testenv.Eventually(t, 30*time.Second, func() error {
		l, _ := e.catalog.Get(ctx, &me, *listings[gone], "vi")
		if l.Status != catalogdomain.StatusHidden {
			return errors.New("not hidden: " + string(l.Status))
		}
		return nil
	})
	// While Chợ Tốt is down, a check changes nothing.
	e.source.setDown(true)
	e.sync(t, me)
	e.source.setDown(false)
	// A copy sold here reminds the seller to hide the listing on Chợ Tốt.
	ev := uuid.NewV7()
	if err = e.svc.ListingSold(ctx, *listings[kept], ev, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM events WHERE id = @id AND kind = 'chotot_import.sold'`, pgx.StrictNamedArgs{"id": ev}); n != 1 {
		t.Fatalf("%d sold events", n)
	}
	var imp uuid.UUID
	if err = e.db.QueryRow(ctx, `SELECT id FROM chotot_imports WHERE listing_id = $1`, *listings[kept]).Scan(&imp); err != nil {
		t.Fatal(err)
	}
	if f, ok, factsErr := e.svc.SaleFacts(ctx, imp); factsErr != nil || !ok || f.Account != me || f.ListID != kept || f.Title == "" {
		t.Fatalf("sale facts: %+v %v %v", f, ok, factsErr)
	}
	if err = e.svc.DeleteAccount(ctx, me); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.svc.Get(ctx, me); st.Link != nil {
		t.Fatal("delete account")
	}
}
