package stream_test

import (
	"bufio"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/stream"
)

var discard = slog.New(slog.DiscardHandler)

// running starts a hub on db until the test ends.
func running(t *testing.T, db *pgxpool.Pool) *stream.Hub {
	t.Helper()
	h := stream.NewHub(db, discard)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = h.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return h
}

func notify(t *testing.T, db *pgxpool.Pool, accounts []uuid.UUID, h stream.Hint, commit bool) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Notify(ctx, tx, accounts, h); err != nil {
		t.Fatal(err)
	}
	if commit {
		err = tx.Commit(ctx)
	} else {
		err = tx.Rollback(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func dealHint() stream.Hint {
	id := uuid.NewV7()
	return stream.Hint{Kind: stream.KindDeal, DealID: &id}
}

// receive notifies until a hint arrives on ch: the hub listens only some
// time after it starts.
func receive(t *testing.T, db *pgxpool.Pool, ch <-chan stream.Hint, accounts []uuid.UUID, h stream.Hint) stream.Hint {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		notify(t, db, accounts, h, true)
		select {
		case got := <-ch:
			return got
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("no hint within 5s")
	return stream.Hint{}
}

func quiet(t *testing.T, ch <-chan stream.Hint) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("an unexpected hint: %+v", got)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestHintReachesOnlyItsAccounts(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	h := running(t, db)
	a, b := uuid.NewV7(), uuid.NewV7()
	ca, stopA := h.Subscribe(a)
	defer stopA()
	cb, stopB := h.Subscribe(b)
	defer stopB()
	want := dealHint()
	if got := receive(t, db, ca, []uuid.UUID{a}, want); got.Kind != stream.KindDeal || *got.DealID != *want.DealID {
		t.Fatalf("%+v", got)
	}
	quiet(t, cb)
}

func TestRollbackSendsNothing(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	h := running(t, db)
	a := uuid.NewV7()
	ca, stop := h.Subscribe(a)
	defer stop()
	receive(t, db, ca, []uuid.UUID{a}, dealHint())
	for len(ca) > 0 {
		<-ca
	}
	notify(t, db, []uuid.UUID{a}, dealHint(), false)
	quiet(t, ca)
}

func TestHandlerStreams(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	h := running(t, db)
	a := uuid.NewV7()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Handler(discard).ServeHTTP(w, r.WithContext(authn.With(r.Context(), authn.Principal{AccountID: a})))
	}))
	srv.Config.WriteTimeout = time.Second
	srv.Start()
	defer srv.Close()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // Closed below; the reading goroutine loses the linter.
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func() string {
		t.Helper()
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("the stream ended")
			}
			return l
		case <-time.After(5 * time.Second):
			t.Fatal("no line within 5s")
		}
		return ""
	}
	if l := next(); l != "event: ready" {
		t.Fatalf("first line %q", l)
	}
	time.Sleep(1500 * time.Millisecond) // past the server's write timeout
	want := dealHint()
	ctx := context.Background()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		tx, txErr := db.Begin(ctx)
		if txErr != nil {
			t.Fatal(txErr)
		}
		if txErr = stream.Notify(ctx, tx, []uuid.UUID{a}, want); txErr != nil {
			t.Fatal(txErr)
		}
		if txErr = tx.Commit(ctx); txErr != nil {
			t.Fatal(txErr)
		}
		for {
			l := next()
			if l == "event: deal" {
				if d := next(); !strings.Contains(d, want.DealID.String()) {
					t.Fatalf("data %q", d)
				}
				return
			}
			if l == "" || strings.HasPrefix(l, "data:") {
				break
			}
		}
	}
	t.Fatal("no deal event")
}

func TestCloseEndsStreams(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	h := running(t, db)
	a := uuid.NewV7()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.Handler(discard).ServeHTTP(w, r.WithContext(authn.With(r.Context(), authn.Principal{AccountID: a})))
	}))
	defer srv.Close()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // Closed below; the reading goroutine loses the linter.
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	ended := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
		}
		close(ended)
	}()
	time.Sleep(200 * time.Millisecond)
	h.Close()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the stream outlived Close")
	}
	if ch, stop := h.Subscribe(a); ch != nil {
		defer stop()
		if _, open := <-ch; open {
			t.Fatal("a subscription after Close is open")
		}
	}
}
