package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/testenv"
)

type listingDraft struct {
	ID            string `json:"id"`
	SchemaVersion int    `json:"schema_version"`
	Title         string `json:"title"`
	Price         *int64 `json:"price"`
	Body          string `json:"body"`
	Photos        []struct {
		UploadID string `json:"upload_id"`
		URL      string `json:"url"`
	} `json:"photos"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// draftBody is a draft's PUT with the given photos.
func draftBody(title string, photos ...string) map[string]any {
	if photos == nil {
		photos = []string{}
	}
	return map[string]any{
		"schema_version": 1, "title": title, "price": 1_500_000, "photos": photos, "body": `{"condition":"used"}`,
	}
}

func TestListingDraftLifecycle(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	auth := bearer(me.AccessToken)
	photo := upload(t, me.AccessToken, photoWithGPS(t))
	path := "/v1/listing-drafts/" + uuid.NewV7().String()

	d := expect[listingDraft](t, call(t, "PUT", path, draftBody("Xe đạp", photo), auth), http.StatusCreated)
	if d.Title != "Xe đạp" || len(d.Photos) != 1 || d.Photos[0].UploadID != photo || d.Body != `{"condition":"used"}` ||
		d.ExpiresAt.Sub(d.UpdatedAt) != catalogdomain.DraftLife {
		t.Fatalf("%+v", d)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, d.Photos[0].URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	got, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = got.Body.Close()
	if got.StatusCode != http.StatusOK {
		t.Fatalf("the photo's URL: %d", got.StatusCode)
	}

	if d = expect[listingDraft](t, call(t, "PUT", path, draftBody("Xe đạp Asama", photo), auth), http.StatusOK); d.Title != "Xe đạp Asama" {
		t.Fatalf("%+v", d)
	}
	list := expect[struct {
		Items []listingDraft `json:"items"`
	}](t, call(t, "GET", "/v1/listing-drafts", nil, auth), http.StatusOK)
	if len(list.Items) != 1 || list.Items[0].Title != "Xe đạp Asama" || len(list.Items[0].Photos) != 1 {
		t.Fatalf("%+v", list)
	}
	check(t, "delete", call(t, "DELETE", path, nil, auth), http.StatusNoContent, "")
	check(t, "delete again", call(t, "DELETE", path, nil, auth), http.StatusNoContent, "")
	if list = expect[struct {
		Items []listingDraft `json:"items"`
	}](t, call(t, "GET", "/v1/listing-drafts", nil, auth), http.StatusOK); len(list.Items) != 0 {
		t.Fatalf("%+v", list)
	}
}

func TestListingDraftRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	auth := bearer(me.AccessToken)
	fresh := func() string { return "/v1/listing-drafts/" + uuid.NewV7().String() }
	theirs := fresh()
	expectStatus(t, call(t, "PUT", theirs, draftBody("Của họ"), bearer(other.AccessToken)), http.StatusCreated)

	check(t, "put signed out", call(t, "PUT", fresh(), draftBody("")), http.StatusUnauthorized, "unauthenticated")
	check(t, "list signed out", call(t, "GET", "/v1/listing-drafts", nil), http.StatusUnauthorized, "unauthenticated")
	check(t, "delete signed out", call(t, "DELETE", fresh(), nil), http.StatusUnauthorized, "unauthenticated")
	check(t, "put a bad ID", call(t, "PUT", "/v1/listing-drafts/nope", draftBody(""), auth), http.StatusBadRequest, "invalid_request")
	check(t, "delete a bad ID", call(t, "DELETE", "/v1/listing-drafts/nope", nil, auth), http.StatusBadRequest, "invalid_request")
	check(t, "a 101-letter title", call(t, "PUT", fresh(), draftBody(strings.Repeat("a", 101)), auth), http.StatusBadRequest, "invalid_request")
	big := draftBody("")
	big["body"] = strings.Repeat("a", 32769)
	check(t, "a body over 32768 characters", call(t, "PUT", fresh(), big, auth), http.StatusBadRequest, "invalid_request")
	check(t, "another account's ID", call(t, "PUT", theirs, draftBody(""), auth), http.StatusNotFound, "draft_not_found")
	check(t, "their photo", call(t, "PUT", fresh(), draftBody("", upload(t, other.AccessToken, photoWithGPS(t))), auth),
		http.StatusUnprocessableEntity, "invalid_upload")
	check(t, "delete another account's", call(t, "DELETE", theirs, nil, auth), http.StatusNoContent, "")
	expectStatus(t, call(t, "GET", "/v1/listing-drafts", nil, bearer(other.AccessToken)), http.StatusOK)

	for range catalogdomain.MaxDrafts {
		expectStatus(t, call(t, "PUT", fresh(), draftBody(""), auth), http.StatusCreated)
	}
	check(t, "the 21st", call(t, "PUT", fresh(), draftBody(""), auth), http.StatusConflict, "draft_limit_reached")
}

// A deleted account's drafts go.
func TestDeletedAccountsDraftsGo(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	expectStatus(t, call(t, "PUT", "/v1/listing-drafts/"+uuid.NewV7().String(), draftBody(""), bearer(me.AccessToken)), http.StatusCreated)
	check(t, "delete", call(t, "DELETE", "/v1/accounts/me", nil, bearer(me.AccessToken)), http.StatusNoContent, "")
	testenv.Eventually(t, 30*time.Second, func() error {
		var n int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM listing_drafts WHERE account_id = @account::uuid`,
			pgx.StrictNamedArgs{"account": me.Account.ID}).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("%d drafts", n)
		}
		return nil
	})
}
