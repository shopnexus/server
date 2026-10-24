package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

type bindAll struct {
	ID     uuid.UUID `json:"-"                 path:"id"`
	Limit  int       `json:"-"                 query:"limit"`
	Unread bool      `json:"-"                 query:"unread"`
	Key    string    `header:"Idempotency-Key" json:"-"`
	Name   string    `json:"name"`
}

func request(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
}

func TestBindReadsEverySource(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	r := request(t, "POST", "/things/x?limit=7&unread=true", `{"name":"a"}`)
	r.SetPathValue("id", id.String())
	r.Header.Set("Idempotency-Key", "k1")

	var got bindAll
	if err := Bind(r, &got); err != nil {
		t.Fatal(err)
	}
	want := bindAll{ID: id, Limit: 7, Unread: true, Key: "k1", Name: "a"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestBindSkipsBodyWithoutBodyFields(t *testing.T) {
	t.Parallel()
	type query struct {
		Cursor uuid.UUID `json:"-" query:"cursor"`
		Limit  int       `json:"-" query:"limit"`
	}
	// A GET has no body; reading one would fail with malformed_json.
	var got query
	if err := Bind(request(t, "GET", "/things", ""), &got); err != nil {
		t.Fatal(err)
	}
	if got != (query{}) {
		t.Fatalf("absent parameters should stay zero, got %+v", got)
	}
}

func TestBindRejectsAValueThatDoesNotParse(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/things?limit=ten", "/things?unread=maybe"} {
		r := request(t, "POST", target, `{"name":"a"}`)
		r.SetPathValue("id", uuid.New().String())
		var got bindAll
		err := Bind(r, &got)
		e, ok := errors.AsType[*apperr.Error](err)
		if !ok || e.Code != "invalid_request" || len(e.Fields) != 1 {
			t.Fatalf("%s: got %v, want invalid_request with one field", target, err)
		}
	}
}

func TestBindRejectsUnknownBodyFields(t *testing.T) {
	t.Parallel()
	var got struct {
		Name string `json:"name"`
	}
	err := Bind(request(t, "POST", "/things", `{"name":"a","extra":1}`), &got)
	if !errors.Is(err, ErrMalformedJSON) {
		t.Fatalf("got %v, want malformed_json", err)
	}
}

func TestBindCachesThePlan(t *testing.T) {
	t.Parallel()
	var got bindAll
	_ = Bind(request(t, "GET", "/", ""), &got)
	first, _ := plans.Load(reflect.TypeFor[bindAll]())
	_ = Bind(request(t, "GET", "/", ""), &got)
	second, _ := plans.Load(reflect.TypeFor[bindAll]())
	if first == nil || first != second {
		t.Fatal("the second call should reuse the first call's plan")
	}
}

func TestBindKeepsOnePlanUnderConcurrentFirstCalls(t *testing.T) {
	t.Parallel()
	type fresh struct {
		Limit int `json:"-" query:"limit"`
	}
	typ := reflect.TypeFor[fresh]()
	// Each round empties the cache for fresh and releases every goroutine
	// at once, so several of them build a plan at the same time.
	for range 200 {
		plans.Delete(typ)
		got := make([]*plan, 16)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range got {
			wg.Go(func() {
				<-start
				got[i] = planOf(typ)
			})
		}
		close(start)
		wg.Wait()
		for _, p := range got {
			if p != got[0] {
				t.Fatal("concurrent first calls got different plans")
			}
		}
	}
}

func TestBindPanicsOnAWrongRequestType(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*http.Request){
		"url field without json:\"-\"": func(r *http.Request) {
			var v struct {
				Limit int `query:"limit"`
			}
			_ = Bind(r, &v)
		},
		"two sources": func(r *http.Request) {
			var v struct {
				Limit int `header:"Limit" json:"-" query:"limit"`
			}
			_ = Bind(r, &v)
		},
		"unsupported type": func(r *http.Request) {
			var v struct {
				Tags []string `json:"-" query:"tags"`
			}
			_ = Bind(r, &v)
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Fatal("Bind did not panic")
				}
			}()
			call(request(t, "GET", "/", ""))
		})
	}
}

type pageQuery struct {
	Cursor uuid.UUID `json:"-" query:"cursor"`
	Limit  int       `json:"-" query:"limit"`
}

func TestBindReadsEmbeddedStructs(t *testing.T) {
	t.Parallel()
	type filtered struct {
		pageQuery

		Status string `json:"-" query:"status"`
	}
	c := uuid.New()
	var got filtered
	if err := Bind(request(t, "GET", "/things?limit=5&status=open&cursor="+c.String(), ""), &got); err != nil {
		t.Fatal(err)
	}
	if got.Cursor != c || got.Limit != 5 || got.Status != "open" {
		t.Fatalf("got %+v", got)
	}

	type withBody struct {
		pageQuery

		Name string `json:"name"`
	}
	var b withBody
	if err := Bind(request(t, "POST", "/things?limit=3", `{"name":"a"}`), &b); err != nil {
		t.Fatal(err)
	}
	if b.Limit != 3 || b.Name != "a" {
		t.Fatalf("got %+v", b)
	}
}

func TestBindReadsACookie(t *testing.T) {
	t.Parallel()
	var got struct {
		Refresh string `cookie:"staff_refresh" json:"-"`
	}
	r := request(t, "POST", "/", "")
	r.AddCookie(&http.Cookie{Name: "staff_refresh", Value: "rt_abc"})
	if err := Bind(r, &got); err != nil || got.Refresh != "rt_abc" {
		t.Fatalf("%+v %v", got, err)
	}
	var none struct {
		Refresh string `cookie:"staff_refresh" json:"-"`
	}
	if err := Bind(request(t, "POST", "/", ""), &none); err != nil || none.Refresh != "" {
		t.Fatalf("no cookie: %+v %v", none, err)
	}
}
