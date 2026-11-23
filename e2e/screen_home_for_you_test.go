package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	discoverydomain "github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/platform/testenv"
)

// homeForYou is what Home holds for Dành cho bạn and Vì bạn đã xem gần đây.
type homeForYou struct {
	ForYou []searchCard `json:"for_you"`
	Viewed *struct {
		Category struct {
			Code        string `json:"code"`
			DisplayName string `json:"display_name"`
		} `json:"category"`
		Items []searchCard `json:"items"`
	} `json:"viewed"`
}

// home reads Home as token, "" for a guest, and fails on a duplicate card.
func home(t *testing.T, token, query string) (homeForYou, Response) {
	t.Helper()
	res := call(t, "GET", "/v1/screens/home?"+query, nil, bearer(token))
	v := expect[homeForYou](t, res, http.StatusOK)
	if ids := cardIDsOf(v.ForYou); len(slices.Compact(slices.Sorted(slices.Values(ids)))) != len(ids) {
		t.Fatalf("a card repeats: %v", ids)
	}
	return v, res
}

func cardIDsOf(cards []searchCard) []string {
	out := make([]string, len(cards))
	for i := range cards {
		out[i] = cards[i].ID
	}
	return out
}

// newestFirst checks that read gives the first size of the newest listings
// token's search shows that keep keeps, or all of them when fewer, a
// seller's repeated title once as Home shows it. The two
// searches around read must agree, or a listing arrived meanwhile and the
// check runs again.
func newestFirst(t *testing.T, token string, size int, read func() []string, keep func(searchCard) bool) {
	t.Helper()
	newest := func() []string {
		var out []string
		seen := map[[2]string]bool{}
		items := expect[searchView](t, call(t, "GET", "/v1/search?sort=newest&limit=100", nil, bearer(token)), http.StatusOK).Items
		for i := range items {
			c := &items[i]
			k := [2]string{c.SellerID, discoverydomain.QueryKey(c.Title)}
			if keep(*c) && !seen[k] {
				seen[k] = true
				out = append(out, c.ID)
			}
		}
		return out
	}
	testenv.Eventually(t, 30*time.Second, func() error {
		before := newest()
		got := read()
		if after := newest(); !slices.Equal(before, after) {
			return errors.New("listings changed during the read")
		}
		if want := before[:min(size, len(before))]; !slices.Equal(got, want) {
			return fmt.Errorf("got %v, want %v", got, want)
		}
		return nil
	})
}

// The test owns baby.highchair, home.washer and sports.camping: no other
// test posts in them, so it knows every listing there.
func TestHomeForYou(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	me, _ := newAccount(t)
	blockedSeller, _ := newAccount(t)
	blocker, _ := newAccount(t)
	solo, _ := newAccount(t)
	fresh, _ := newAccount(t)
	// Each its own title: the feed shows a seller's repeated title once.
	n := 0
	post := func(token, category string) string {
		t.Helper()
		n++
		return activeListing(t, token, map[string]any{"category_code": category, "title": fmt.Sprintf("Món %d %s", n, category)}).ID
	}
	chairOld := post(seller.AccessToken, "baby.highchair")
	chairNew := post(seller.AccessToken, "baby.highchair")
	washer := post(seller.AccessToken, "home.washer")
	tentOld := post(seller.AccessToken, "sports.camping")
	tentNew := post(seller.AccessToken, "sports.camping")
	own := post(me.AccessToken, "sports.camping")
	hidden := post(seller.AccessToken, "sports.camping")
	blocked := post(blockedSeller.AccessToken, "sports.camping")
	blocking := post(blocker.AccessToken, "sports.camping")

	auth := bearer(me.AccessToken)
	expectStatus(t, call(t, "PUT", "/v1/hidden/"+hidden, nil, auth), http.StatusNoContent)
	expectStatus(t, call(t, "POST", "/v1/blocks", map[string]any{"account_id": blockedSeller.Account.ID}, auth), http.StatusNoContent)
	expectStatus(t, call(t, "POST", "/v1/blocks", map[string]any{"account_id": me.Account.ID}, bearer(blocker.AccessToken)),
		http.StatusNoContent)
	// A save weighs 3, a search with a category 2 and an opened listing 1.
	expectStatus(t, call(t, "PUT", "/v1/saved/"+chairOld, nil, auth), http.StatusNoContent)
	if res, _ := search(t, url.Values{"q": {"may giat cu"}}, auth); res.Status != http.StatusOK {
		t.Fatalf("search: %d %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "GET", "/v1/screens/listing/"+tentOld, nil, auth), http.StatusOK)
	expectStatus(t, call(t, "GET", "/v1/screens/listing/"+washer, nil, bearer(solo.AccessToken)), http.StatusOK)

	v, res := home(t, me.AccessToken, "near_commune=00004")
	got := cardIDsOf(v.ForYou)
	if want := []string{chairNew, chairOld, washer, tentNew, tentOld}; len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
		t.Fatalf("the weighed subcategories first: %v, want %v first", got, want)
	}
	if !v.ForYou[1].Saved || v.ForYou[0].Saved {
		t.Fatalf("saved marks: %s", res.Body)
	}
	for _, c := range v.ForYou {
		if c.DistanceKm == nil {
			t.Fatalf("a card without its distance: %s", res.Body)
		}
	}
	for _, id := range []string{own, hidden, blocked, blocking} {
		if slices.Contains(got, id) || (v.Viewed != nil && slices.Contains(cardIDsOf(v.Viewed.Items), id)) {
			t.Fatalf("%s is the viewer's own, hidden or blocked: %s", id, res.Body)
		}
	}
	if v.Viewed == nil || v.Viewed.Category.Code != "sports.camping" || v.Viewed.Category.DisplayName != "Cắm trại" ||
		!slices.Equal(cardIDsOf(v.Viewed.Items), []string{tentNew}) {
		t.Fatalf("viewed: %s", res.Body)
	}
	weighed := []string{"baby.highchair", "home.washer", "sports.camping"}
	newestFirst(t, me.AccessToken, 20-5, func() []string {
		h, _ := home(t, me.AccessToken, "")
		if len(h.ForYou) < 5 {
			return cardIDsOf(h.ForYou)
		}
		return cardIDsOf(h.ForYou[5:])
	}, func(c searchCard) bool {
		return !slices.Contains(weighed, c.CategoryCode) && c.SellerID != me.Account.ID
	})

	// A guest and an account with no signals get the newest listings, and
	// no rail.
	for name, token := range map[string]string{"a guest": "", "no signals": fresh.AccessToken} {
		newestFirst(t, token, 20, func() []string {
			h, read := home(t, token, "")
			if h.Viewed != nil || bytes.Contains(read.Body, []byte(`"viewed"`)) {
				t.Fatalf("%s has a rail: %s", name, read.Body)
			}
			if len(h.ForYou) == 0 || h.ForYou[0].DistanceKm != nil {
				t.Fatalf("%s: %s", name, read.Body)
			}
			return cardIDsOf(h.ForYou)
		}, func(searchCard) bool { return true })
	}
	// solo opened the only washer, so the rail has nothing to show.
	if v, res = home(t, solo.AccessToken, ""); v.Viewed != nil || bytes.Contains(res.Body, []byte(`"viewed"`)) {
		t.Fatalf("solo has a rail: %s", res.Body)
	}
}
