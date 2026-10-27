package e2e

import (
	"net/http"
	"testing"
)

func TestHomeScreen(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	postActive(t, seller.AccessToken, nil)
	res := call(t, server, "GET", "/v1/screens/home?near_commune=00004", nil)
	v := decode[struct {
		Categories []struct {
			Code     string `json:"code"`
			Count    int    `json:"count"`
			Children []struct {
				Code string `json:"code"`
			} `json:"children"`
		} `json:"categories"`
		Nearby searchView `json:"nearby"`
	}](t, res)
	if res.Status != http.StatusOK || len(v.Categories) != 9 || len(v.Nearby.Items) == 0 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	counted := false
	for _, c := range v.Categories {
		if c.Code == "vehicles" && c.Count >= 1 && len(c.Children) > 0 {
			counted = true
		}
	}
	if !counted {
		t.Fatalf("vehicles is not counted: %s", res.Body)
	}
	for name, c := range map[string]struct {
		path    string
		headers [][2]string
		status  int
		code    string
	}{
		"no position":        {"/v1/screens/home", nil, 400, "position_required"},
		"an unknown commune": {"/v1/screens/home?near_commune=nope", nil, 400, "invalid_position"},
		"a bad token":        {"/v1/screens/home?near_commune=00004", [][2]string{{"Authorization", "Bearer nope"}}, 401, ""},
	} {
		res := call(t, server, "GET", c.path, nil, c.headers...)
		if res.Status != c.status || (c.code != "" && errorOf(t, res).Code != c.code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
}
