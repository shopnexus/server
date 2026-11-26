package e2e

import (
	"net/http"
	"testing"
)

func TestHomeScreen(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	activeListing(t, seller.AccessToken, nil)
	res := call(t, "GET", "/v1/screens/home?near_commune=00004", nil)
	v := decode[struct {
		Categories []struct {
			Code     string `json:"code"`
			Children []struct {
				Code string `json:"code"`
			} `json:"children"`
		} `json:"categories"`
		Nearby searchView `json:"nearby"`
	}](t, res)
	if res.Status != http.StatusOK || len(v.Categories) != 14 || len(v.Nearby.Items) == 0 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if c := v.Categories[0]; c.Code != "phones" || len(c.Children) == 0 || c.Children[0].Code != "phones.iphone" {
		t.Fatalf("the tree: %s", res.Body)
	}

	// Without a position the view still answers, with an empty feed. The
	// categories are named in the request's language.
	res = call(t, "GET", "/v1/screens/home", nil, [2]string{"Accept-Language", "en"})
	if bare := decode[struct {
		Categories []struct {
			DisplayName string `json:"display_name"`
			Children    []struct {
				DisplayName string `json:"display_name"`
			} `json:"children"`
		} `json:"categories"`
		Nearby searchView `json:"nearby"`
	}](t, res); res.Status != http.StatusOK || len(bare.Categories) != 14 || bare.Nearby.Items == nil || len(bare.Nearby.Items) != 0 ||
		bare.Nearby.NextCursor != "" || bare.Categories[0].DisplayName != "Phones" || bare.Categories[0].Children[0].DisplayName != "iPhone" ||
		res.Header.Get("Vary") != "Accept-Language" {
		t.Fatalf("no position: %d %s", res.Status, res.Body)
	}
	// radius_km bounds the feed as it bounds the search.
	res = call(t, "GET", "/v1/screens/home?near_commune=00004&radius_km=1", nil)
	if near := decode[struct {
		Nearby searchView `json:"nearby"`
	}](t, res); res.Status != http.StatusOK || len(near.Nearby.Items) == 0 {
		t.Fatalf("radius_km: %d %s", res.Status, res.Body)
	} else {
		for _, it := range near.Nearby.Items {
			if it.DistanceKm == nil || *it.DistanceKm > 1 {
				t.Fatalf("radius_km=1 let a listing farther away in: %s", res.Body)
			}
		}
	}
	for name, c := range map[string]struct {
		path    string
		headers [][2]string
		status  int
		code    string
	}{
		"a radius alone":     {"/v1/screens/home?radius_km=5", nil, 400, "position_required"},
		"a radius of 0":      {"/v1/screens/home?near_commune=00004&radius_km=0", nil, 400, ""},
		"a radius over 200":  {"/v1/screens/home?near_commune=00004&radius_km=201", nil, 400, ""},
		"an unknown commune": {"/v1/screens/home?near_commune=nope", nil, 400, "invalid_position"},
		"a bad token":        {"/v1/screens/home?near_commune=00004", [][2]string{{"Authorization", "Bearer nope"}}, 401, ""},
	} {
		res := call(t, "GET", c.path, nil, c.headers...)
		if res.Status != c.status || (c.code != "" && errorOf(t, res).Code != c.code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
}
