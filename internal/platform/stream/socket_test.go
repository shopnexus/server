package stream_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/stream"
)

type fakeAccess struct {
	mu        sync.Mutex
	suspended map[uuid.UUID]bool
	peers     map[uuid.UUID]uuid.UUID // conversation → whom typing there reaches
	touched   map[uuid.UUID]int
	peerCalls int
	peerDelay time.Duration // how long Peer keeps the connection's main loop busy
}

func (f *fakeAccess) Suspended(_ context.Context, account uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suspended[account], nil
}

func (f *fakeAccess) Peer(_ context.Context, _, conversation uuid.UUID) (uuid.UUID, bool, error) {
	f.mu.Lock()
	f.peerCalls++
	delay := f.peerDelay
	to, ok := f.peers[conversation]
	f.mu.Unlock()
	time.Sleep(delay)
	return to, ok, nil
}

func (f *fakeAccess) peerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.peerCalls
}

func (f *fakeAccess) Touch(_ context.Context, account uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched[account]++
	return nil
}

func (f *fakeAccess) touches(account uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.touched[account]
}

func (f *fakeAccess) suspend(account uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspended[account] = true
}

// socketEnv serves a Socket on a fresh database. A request's bearer token
// names a principal the test registered with token.
type socketEnv struct {
	db     *pgxpool.Pool
	hub    *stream.Hub
	access *fakeAccess
	url    string
	mu     sync.Mutex
	tokens map[string]authn.Principal
}

// newSocketEnv starts the socket with a 300 ms tick; opts change it before
// it serves.
func newSocketEnv(t *testing.T, opts ...func(*stream.Socket)) *socketEnv {
	t.Helper()
	db := pgtest.New(t)
	e := &socketEnv{
		db: db, tokens: map[string]authn.Principal{},
		access: &fakeAccess{suspended: map[uuid.UUID]bool{}, peers: map[uuid.UUID]uuid.UUID{}, touched: map[uuid.UUID]int{}},
	}
	e.hub = stream.NewHub(stream.PGBell{DB: db, Log: discard}, stream.PGLog{DB: db}, time.Now, discard)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = e.hub.Run(ctx)
		close(done)
	}()
	verify := func(tok string, _ time.Time) (authn.Principal, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		p, ok := e.tokens[tok]
		if !ok {
			return authn.Principal{}, errors.New("unknown token")
		}
		return p, nil
	}
	socket := stream.Socket{
		Hub: e.hub, Updates: stream.PGLog{DB: db}, Access: e.access, Verify: verify,
		Tick: 300 * time.Millisecond, Now: time.Now, Log: discard,
	}
	for _, opt := range opts {
		opt(&socket)
	}
	updates := stream.UpdatesHandler(stream.PGLog{DB: db}, discard)
	srv := httptest.NewUnstartedServer(stream.SubprotocolToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), time.Now())
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		r = r.WithContext(authn.With(r.Context(), p))
		if r.URL.Path == "/updates" {
			updates.ServeHTTP(w, r)
			return
		}
		socket.ServeHTTP(w, r)
	})))
	// Shorter than the tests wait, so a socket that kept them would end.
	srv.Config.ReadTimeout, srv.Config.WriteTimeout = time.Second, time.Second
	srv.Start()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		cancel()
		<-done
	})
	e.url = srv.URL
	return e
}

// token registers a principal for account valid for ttl and returns its
// token.
func (e *socketEnv) token(account uuid.UUID, ttl time.Duration) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	tok := uuid.NewV7().String()
	e.tokens[tok] = authn.Principal{AccountID: account, ExpiresAt: time.Now().Add(ttl)}
	return tok
}

func (e *socketEnv) notify(t *testing.T, account uuid.UUID, n int) {
	t.Helper()
	err := pgx.BeginFunc(context.Background(), e.db, func(tx pgx.Tx) error {
		for range n {
			if err := stream.Notify(context.Background(), tx, []uuid.UUID{account}, dealHint()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type frame struct {
	Type           string     `json:"type"`
	Seq            int64      `json:"seq"`
	Kind           string     `json:"kind"`
	ConversationID *uuid.UUID `json:"conversation_id"`
	DealID         *uuid.UUID `json:"deal_id"`
}

type client struct {
	conn   *websocket.Conn
	frames chan frame
	err    chan error
}

// dial opens the socket as token with query ("" or "?since=N") and reads
// its frames until it closes.
func (e *socketEnv) dial(t *testing.T, token, query string) *client {
	t.Helper()
	return e.dialWith(t, query, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
}

// dialWith opens the socket with opts, which carry the token.
func (e *socketEnv) dialWith(t *testing.T, query string, opts *websocket.DialOptions) *client {
	t.Helper()
	c, _, err := websocket.Dial(context.Background(), e.url+"/ws"+query, opts) //nolint:bodyclose // Dial closes the handshake response's body.
	if err != nil {
		t.Fatal(err)
	}
	cl := &client{conn: c, frames: make(chan frame, 1024), err: make(chan error, 1)}
	go func() {
		for {
			_, b, readErr := c.Read(context.Background())
			if readErr != nil {
				cl.err <- readErr
				close(cl.frames)
				return
			}
			var f frame
			if jsonErr := json.Unmarshal(b, &f); jsonErr != nil {
				cl.err <- jsonErr
				close(cl.frames)
				return
			}
			cl.frames <- f
		}
	}()
	t.Cleanup(func() { _ = c.CloseNow() })
	return cl
}

func (cl *client) next(t *testing.T) frame {
	t.Helper()
	select {
	case f, ok := <-cl.frames:
		if !ok {
			t.Fatalf("the socket closed: %v", <-cl.err)
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5s")
	}
	return frame{}
}

func (cl *client) quiet(t *testing.T) {
	t.Helper()
	select {
	case f, ok := <-cl.frames:
		if ok {
			t.Fatalf("an unexpected frame: %+v", f)
		}
	case <-time.After(500 * time.Millisecond):
	}
}

// closedWith waits for the socket to close and returns its close code.
func (cl *client) closedWith(t *testing.T) websocket.StatusCode {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-cl.frames:
			if !ok {
				return websocket.CloseStatus(<-cl.err)
			}
		case <-deadline:
			t.Fatal("the socket did not close within 5s")
		}
	}
}

func (cl *client) send(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err = cl.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func expectFrame(t *testing.T, f frame, typ string, seq int64) {
	t.Helper()
	if f.Type != typ || f.Seq != seq {
		t.Fatalf("frame %+v, want %s %d", f, typ, seq)
	}
}

func TestSocketCatchesUpThenGoesLive(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	e.notify(t, a, 3)
	cl := e.dial(t, e.token(a, time.Hour), "?since=1")
	expectFrame(t, cl.next(t), "update", 2)
	if f := cl.next(t); f.Type != "update" || f.Seq != 3 || f.Kind != stream.KindDeal || f.DealID == nil {
		t.Fatalf("%+v", f)
	}
	expectFrame(t, cl.next(t), "ready", 3)
	time.Sleep(1500 * time.Millisecond) // past the server's read and write timeouts
	e.notify(t, a, 1)
	expectFrame(t, cl.next(t), "update", 4)
	if e.access.touches(a) == 0 {
		t.Fatal("no touch at connect")
	}
}

func TestSocketWithoutSinceStartsNow(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	e.notify(t, a, 2)
	cl := e.dial(t, e.token(a, time.Hour), "")
	expectFrame(t, cl.next(t), "ready", 2)
	e.notify(t, a, 1)
	expectFrame(t, cl.next(t), "update", 3)
}

func TestSocketResetsASinceAheadOfTheLog(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	e.notify(t, a, 2)
	cl := e.dial(t, e.token(a, time.Hour), "?since=9")
	expectFrame(t, cl.next(t), "reset", 2)
	expectFrame(t, cl.next(t), "ready", 2)
}

func TestSocketPagesThroughALongBacklog(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	e.notify(t, a, 501)
	cl := e.dial(t, e.token(a, time.Hour), "?since=0")
	for seq := int64(1); seq <= 501; seq++ {
		expectFrame(t, cl.next(t), "update", seq)
	}
	expectFrame(t, cl.next(t), "ready", 501)
}

// Updates rung while the socket catches up reach it once each, in order.
func TestSocketSendsNoDuplicates(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	e.notify(t, a, 50)
	cl := e.dial(t, e.token(a, time.Hour), "?since=0")
	for range 20 {
		e.notify(t, a, 1)
	}
	want := int64(1)
	for want <= 70 {
		f := cl.next(t)
		if f.Type == "ready" {
			continue
		}
		expectFrame(t, f, "update", want)
		want++
	}
	cl.quiet(t)
}

func TestTwoDevicesBothGetUpdates(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	phone := e.dial(t, e.token(a, time.Hour), "")
	tablet := e.dial(t, e.token(a, time.Hour), "")
	expectFrame(t, phone.next(t), "ready", 0)
	expectFrame(t, tablet.next(t), "ready", 0)
	e.notify(t, a, 1)
	expectFrame(t, phone.next(t), "update", 1)
	expectFrame(t, tablet.next(t), "update", 1)
}

func TestTyping(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a, b := uuid.NewV7(), uuid.NewV7()
	conv := uuid.NewV7()
	e.access.mu.Lock()
	e.access.peers[conv] = b
	e.access.mu.Unlock()
	typist := e.dial(t, e.token(a, time.Hour), "")
	reader := e.dial(t, e.token(b, time.Hour), "")
	expectFrame(t, typist.next(t), "ready", 0)
	expectFrame(t, reader.next(t), "ready", 0)
	typist.send(t, map[string]any{"type": "typing", "conversation_id": conv})
	typist.send(t, map[string]any{"type": "typing", "conversation_id": conv}) // within 3 s: dropped
	if f := reader.next(t); f.Type != "typing" || f.ConversationID == nil || *f.ConversationID != conv {
		t.Fatalf("%+v", f)
	}
	reader.quiet(t)
	typist.send(t, map[string]any{"type": "typing", "conversation_id": uuid.NewV7()}) // no peer: dropped
	reader.quiet(t)
	typist.quiet(t)
}

func TestAuthMovesTheExpiry(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	cl := e.dial(t, e.token(a, 2*time.Second), "")
	expectFrame(t, cl.next(t), "ready", 0)
	cl.send(t, map[string]any{"type": "auth", "token": e.token(a, time.Hour)})
	time.Sleep(3 * time.Second)
	e.notify(t, a, 1)
	expectFrame(t, cl.next(t), "update", 1)
	cl.send(t, map[string]any{"type": "auth", "token": e.token(uuid.NewV7(), time.Hour)})
	if code := cl.closedWith(t); code != stream.CloseToken {
		t.Fatalf("another account's token: %d", code)
	}
}

func TestExpiryCloses4001(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	cl := e.dial(t, e.token(a, time.Second), "")
	if code := cl.closedWith(t); code != stream.CloseToken {
		t.Fatalf("%d", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for e.access.touches(a) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("no touch at close")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSuspendedAccountCloses4003(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	e.access.suspend(a)
	cl := e.dial(t, e.token(a, time.Hour), "")
	if code := cl.closedWith(t); code != stream.CloseSuspended {
		t.Fatalf("%d", code)
	}
}

func TestSuspensionClosesAtTheNextAuth(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	cl := e.dial(t, e.token(a, time.Hour), "")
	expectFrame(t, cl.next(t), "ready", 0)
	e.access.suspend(a)
	cl.send(t, map[string]any{"type": "auth", "token": e.token(a, time.Hour)})
	if code := cl.closedWith(t); code != stream.CloseSuspended {
		t.Fatalf("%d", code)
	}
}

func TestMalformedFrameCloses1008(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	for _, c := range []struct {
		name string
		typ  websocket.MessageType
		body string
	}{
		{"not json", websocket.MessageText, "hello"},
		{"binary", websocket.MessageBinary, `{"type":"typing"}`},
		{"unknown type", websocket.MessageText, `{"type":"shout"}`},
	} {
		cl := e.dial(t, e.token(a, time.Hour), "")
		expectFrame(t, cl.next(t), "ready", 0)
		if err := cl.conn.Write(context.Background(), c.typ, []byte(c.body)); err != nil {
			t.Fatal(err)
		}
		if code := cl.closedWith(t); code != websocket.StatusPolicyViolation {
			t.Fatalf("%s: %d", c.name, code)
		}
	}
}

func TestOversizeFrameCloses1009(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	cl := e.dial(t, e.token(uuid.NewV7(), time.Hour), "")
	expectFrame(t, cl.next(t), "ready", 0)
	if err := cl.conn.Write(context.Background(), websocket.MessageText, []byte(strings.Repeat("x", 5000))); err != nil {
		t.Fatal(err)
	}
	if code := cl.closedWith(t); code != websocket.StatusMessageTooBig {
		t.Fatalf("%d", code)
	}
}

func TestHubCloseCloses1001(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	cl := e.dial(t, e.token(uuid.NewV7(), time.Hour), "")
	expectFrame(t, cl.next(t), "ready", 0)
	e.hub.Close()
	if code := cl.closedWith(t); code != websocket.StatusGoingAway {
		t.Fatalf("%d", code)
	}
}

func TestUpdatesHandler(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	e.notify(t, a, 3)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.url+"/updates?since=1", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.token(a, time.Hour))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var p stream.Page
	if err = json.NewDecoder(res.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || len(p.Updates) != 2 || p.Updates[0].Seq != 2 || p.Seq != 3 || p.Reset || p.HasMore {
		t.Fatalf("%d %+v", res.StatusCode, p)
	}
}

// slowLog is the update log behind a pause, as on a slow database.
type slowLog struct {
	stream.PGLog

	delay time.Duration
}

func (l slowLog) Since(ctx context.Context, account uuid.UUID, since int64, limit int) (stream.Page, error) {
	time.Sleep(l.delay)
	return l.PGLog.Since(ctx, account, since, limit)
}

// A client that floods frames is closed before it floods the database.
func TestFrameFloodCloses1008(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	cl := e.dial(t, e.token(uuid.NewV7(), time.Hour), "")
	expectFrame(t, cl.next(t), "ready", 0)
	for range 30 {
		b, _ := json.Marshal(map[string]any{"type": "typing", "conversation_id": uuid.NewV7()})
		if err := cl.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
			break // closed already
		}
	}
	if code := cl.closedWith(t); code != websocket.StatusPolicyViolation {
		t.Fatalf("%d", code)
	}
	if n := e.access.peerCount(); n > 11 {
		t.Fatalf("%d peer lookups", n)
	}
}

// An update whose ring was lost reaches the app at the next tick.
func TestSocketRechecksTheLogWithoutARing(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	cl := e.dial(t, e.token(a, time.Hour), "")
	expectFrame(t, cl.next(t), "ready", 0)
	if _, err := e.db.Exec(context.Background(), `
WITH next AS (
    INSERT INTO account_update_seqs (account_id, seq) VALUES (@a, 1)
    ON CONFLICT (account_id) DO UPDATE SET seq = account_update_seqs.seq + 1
    RETURNING seq)
INSERT INTO account_updates (account_id, seq, kind) SELECT @a, seq, 'deal' FROM next`,
		pgx.StrictNamedArgs{"a": a}); err != nil {
		t.Fatal(err)
	}
	expectFrame(t, cl.next(t), "update", 1)
}

// The socket answers pings while its main loop waits on the database.
func TestPongWhileTheMainLoopIsBusy(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	e.access.mu.Lock()
	e.access.peerDelay = 3 * time.Second
	e.access.mu.Unlock()
	cl := e.dial(t, e.token(uuid.NewV7(), time.Hour), "")
	expectFrame(t, cl.next(t), "ready", 0)
	cl.send(t, map[string]any{"type": "typing", "conversation_id": uuid.NewV7()})
	cl.send(t, map[string]any{"type": "typing", "conversation_id": uuid.NewV7()})
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := cl.conn.Ping(ctx); err != nil {
		t.Fatalf("no pong while the main loop is busy: %v", err)
	}
}

// The socket answers pings while it catches up.
func TestPongDuringCatchUp(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t, func(s *stream.Socket) {
		s.Updates = slowLog{DB: s.Updates.(stream.PGLog).DB, delay: 3 * time.Second}
	})
	a := uuid.NewV7()
	e.notify(t, a, 1)
	cl := e.dial(t, e.token(a, time.Hour), "?since=0")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := cl.conn.Ping(ctx); err != nil {
		t.Fatalf("no pong during catch-up: %v", err)
	}
	expectFrame(t, cl.next(t), "update", 1)
}

// refused dials with opts and returns the handshake's status.
func (e *socketEnv) refused(t *testing.T, opts *websocket.DialOptions) int {
	t.Helper()
	c, resp, err := websocket.Dial(context.Background(), e.url+"/ws", opts) //nolint:bodyclose // Dial closes the handshake response's body.
	if err == nil {
		_ = c.CloseNow()
		t.Fatal("the socket opened")
	}
	if resp == nil {
		t.Fatalf("no handshake response: %v", err)
	}
	return resp.StatusCode
}

func TestHeaderTokenSelectsNoSubprotocol(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	cl := e.dial(t, e.token(uuid.NewV7(), time.Hour), "")
	expectFrame(t, cl.next(t), "ready", 0)
	if got := cl.conn.Subprotocol(); got != "" {
		t.Fatalf("subprotocol %q, want none", got)
	}
}

func TestSubprotocolTokenConnects(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	a := uuid.NewV7()
	cl := e.dialWith(t, "", &websocket.DialOptions{Subprotocols: []string{"bearer", e.token(a, time.Hour)}})
	expectFrame(t, cl.next(t), "ready", 0)
	if got := cl.conn.Subprotocol(); got != "bearer" {
		t.Fatalf("subprotocol %q, want bearer", got)
	}
	e.notify(t, a, 1)
	expectFrame(t, cl.next(t), "update", 1)
}

func TestBadSubprotocolTokenIsRefusedAsABadHeader(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t)
	header := e.refused(t, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer nope"}}})
	sub := e.refused(t, &websocket.DialOptions{Subprotocols: []string{"bearer", "nope"}})
	if header != http.StatusUnauthorized || sub != header {
		t.Fatalf("header %d, subprotocol %d, want both 401", header, sub)
	}
}

func TestOrigins(t *testing.T) {
	t.Parallel()
	e := newSocketEnv(t, func(s *stream.Socket) { s.Origins = []string{"https://site.example.test"} })
	tok := e.token(uuid.NewV7(), time.Hour)
	cl := e.dialWith(t, "", &websocket.DialOptions{
		Subprotocols: []string{"bearer", tok}, HTTPHeader: http.Header{"Origin": {"https://site.example.test"}},
	})
	expectFrame(t, cl.next(t), "ready", 0)
	for _, origin := range []string{"https://evil.example.test", "http://site.example.test"} {
		got := e.refused(t, &websocket.DialOptions{
			Subprotocols: []string{"bearer", tok}, HTTPHeader: http.Header{"Origin": {origin}},
		})
		if got != http.StatusForbidden {
			t.Fatalf("origin %s: status %d, want 403", origin, got)
		}
	}
}

func TestSubprotocolToken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, auth, protocols, want string
	}{
		{"bearer and a token", "", "bearer, tok", "Bearer tok"},
		{"the header wins", "Bearer head", "bearer, tok", "Bearer head"},
		{"bearer alone", "", "bearer", ""},
		{"a third protocol", "", "bearer, tok, more", ""},
		{"another protocol first", "", "chat, tok", ""},
		{"none", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got string
			h := stream.SubprotocolToken(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("Authorization")
			}))
			r := httptest.NewRequest(http.MethodGet, "/v1/ws", nil)
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			if tc.protocols != "" {
				r.Header.Set("Sec-WebSocket-Protocol", tc.protocols)
			}
			h.ServeHTTP(httptest.NewRecorder(), r)
			if got != tc.want {
				t.Fatalf("Authorization %q, want %q", got, tc.want)
			}
		})
	}
}
