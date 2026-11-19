package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/coder/websocket"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

const (
	// CloseToken ends a connection whose access token expired, is not
	// valid, or is another account's; the app refreshes and reconnects.
	CloseToken websocket.StatusCode = 4001
	// CloseSuspended ends a suspended account's connection; the app does
	// not reconnect.
	CloseSuspended websocket.StatusCode = 4003

	frameLimit = 4 << 10
	// A connection may send frameBurst frames at once and frameRate a
	// second after that; past it, it is closed with 1008.
	frameBurst = 10
	frameRate  = 1
	// frameQueue frames wait for a busy main loop; more are dropped, so the
	// reader never stops reading and pongs keep coming.
	frameQueue  = 16
	pageSize    = 500
	pingEvery   = 25 * time.Second
	pongWithin  = 10 * time.Second
	typingEvery = 3 * time.Second
	peerFor     = 30 * time.Second
	// maxPeers bounds the typing caches of one connection.
	maxPeers = 64
)

// Access is what a connection asks the modules; app adapts account and
// chat to it.
type Access interface {
	Suspended(ctx context.Context, account uuid.UUID) (bool, error)
	// Peer returns whom account's typing in conversation reaches: the other
	// member, when account is a member and neither blocks the other.
	Peer(ctx context.Context, account, conversation uuid.UUID) (other uuid.UUID, ok bool, err error)
	// Touch records that account has the app open now.
	Touch(ctx context.Context, account uuid.UUID) error
}

// Socket serves GET /v1/ws behind SubprotocolToken and the auth middleware.
type Socket struct {
	Hub     *Hub
	Updates UpdateLog
	Access  Access
	// Verify checks the tokens a connection renews itself with, as the auth
	// middleware checked the first.
	Verify func(token string, now time.Time) (authn.Principal, error)
	// Tick is how often a connection records that the person is active and
	// reads the log, in case a ring was lost.
	Tick time.Duration
	// Origins are the website's origins (WEBSITE_ORIGINS), whose browsers
	// may open the socket. A request without Origin, as the app's, is let in.
	Origins []string
	Now     func() time.Time
	Log     *slog.Logger
}

// Subprotocol is the WebSocket subprotocol a browser offers, with its access
// token after it, since a browser cannot set Authorization on a WebSocket.
const Subprotocol = "bearer"

// SubprotocolToken puts the token of a request offering exactly
// "bearer, <token>" as its subprotocols into its Authorization header, for
// the auth middleware after it. A request with Authorization keeps it.
func SubprotocolToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			var offered []string
			for _, v := range r.Header.Values("Sec-WebSocket-Protocol") {
				for p := range strings.SplitSeq(v, ",") {
					offered = append(offered, strings.TrimSpace(p))
				}
			}
			if len(offered) == 2 && strings.EqualFold(offered[0], Subprotocol) && offered[1] != "" {
				r = r.Clone(r.Context())
				r.Header.Set("Authorization", "Bearer "+offered[1])
			}
		}
		next.ServeHTTP(w, r)
	})
}

type (
	// seqFrame is ready or reset.
	seqFrame struct {
		Type string `json:"type"`
		Seq  int64  `json:"seq"`
	}
	updateFrame struct {
		Update

		Type string `json:"type"`
	}
	typingFrame struct {
		Type           string     `json:"type"`
		ConversationID *uuid.UUID `json:"conversation_id"`
	}
	// clientFrame is a frame from the app: typing or auth.
	clientFrame struct {
		Type           string     `json:"type"`
		ConversationID *uuid.UUID `json:"conversation_id"`
		Token          string     `json:"token"`
	}
	// peer is an answer of Access.Peer, kept for peerFor.
	peer struct {
		to uuid.UUID
		ok bool
		at time.Time
	}
)

// ServeHTTP upgrades the request and sends the caller's updates after
// since, or none without since, then ready with the seq the app is at, then
// each update and typing frame as it comes, until the app leaves, its token
// expires, or the hub closes.
func (s Socket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, s.Log)
	if !ok {
		return
	}
	ctx := r.Context()
	since := int64(-1) // the validator allows only 0 and up
	if q := r.URL.Query().Get("since"); q != "" {
		v, err := strconv.ParseInt(q, 10, 64)
		if err != nil {
			httpx.WriteError(w, r, s.Log, fmt.Errorf("parse since: %w", err))
			return
		}
		since = v
	}
	suspended, err := s.Access.Suspended(ctx, p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, s.Log, fmt.Errorf("check suspension: %w", err))
		return
	}
	// The server's read and write timeouts would end the connection.
	rc := http.NewResponseController(w)
	if err = errors.Join(rc.SetReadDeadline(time.Time{}), rc.SetWriteDeadline(time.Time{})); err != nil {
		httpx.WriteError(w, r, s.Log, fmt.Errorf("clear the deadlines: %w", err))
		return
	}
	var workers sync.WaitGroup
	defer workers.Wait()
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{Subprotocol}, OriginPatterns: s.Origins,
	})
	if err != nil {
		s.Log.WarnContext(ctx, "stream: accept the websocket", "err", err)
		return
	}
	defer func() { _ = c.CloseNow() }()
	if suspended {
		_ = c.Close(CloseSuspended, "account_suspended")
		return
	}
	c.SetReadLimit(frameLimit)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sub, stop := s.Hub.Subscribe(p.AccountID)
	defer stop()

	write := func(v any) bool {
		b, encErr := json.Marshal(v)
		if encErr != nil {
			s.Log.ErrorContext(ctx, "stream: encode a frame", "err", encErr)
			return false
		}
		return c.Write(ctx, websocket.MessageText, b) == nil
	}
	// The reader and the pinger run from here on, a catch-up included: a
	// connection that stops reading stops answering pings.
	frames := make(chan clientFrame, frameQueue)
	workers.Go(func() {
		defer cancel()
		allowance, refilled := float64(frameBurst), s.Now()
		for {
			typ, b, readErr := c.Read(ctx)
			if readErr != nil {
				return
			}
			var f clientFrame
			if typ != websocket.MessageText || json.Unmarshal(b, &f) != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "a frame is a JSON text message")
				return
			}
			now := s.Now()
			allowance = min(frameBurst, allowance+now.Sub(refilled).Seconds()*frameRate)
			refilled = now
			if allowance < 1 {
				_ = c.Close(websocket.StatusPolicyViolation, "too many frames")
				return
			}
			allowance--
			select {
			case frames <- f:
			default:
				// The main loop is busy: a typing or auth frame is lost, never
				// a pong.
			}
		}
	})
	workers.Go(func() {
		tick := time.NewTicker(pingEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				pingCtx, done := context.WithTimeout(ctx, pongWithin)
				pingErr := c.Ping(pingCtx)
				done()
				if pingErr != nil {
					cancel()
					return
				}
			}
		}
	})

	last := since
	// catchUp sends the log after last, page by page, or reset with the
	// current seq when the log no longer holds what follows last.
	catchUp := func() bool {
		for {
			page, readErr := s.Updates.Since(ctx, p.AccountID, last, pageSize)
			if readErr != nil {
				if ctx.Err() == nil {
					s.Log.WarnContext(ctx, "stream: read the update log", "err", readErr)
				}
				return false
			}
			if page.Reset {
				last = page.Seq
				return write(seqFrame{Type: "reset", Seq: page.Seq})
			}
			for _, u := range page.Updates {
				if !write(updateFrame{Type: "update", Update: u}) {
					return false
				}
				last = u.Seq
			}
			if !page.HasMore {
				return true
			}
		}
	}
	if since < 0 {
		if last, err = s.Updates.Current(ctx, p.AccountID); err != nil {
			s.Log.WarnContext(ctx, "stream: read the current seq", "err", err)
			return
		}
	} else if !catchUp() {
		return
	}
	if !write(seqFrame{Type: "ready", Seq: last}) {
		return
	}

	if err = s.Access.Touch(ctx, p.AccountID); err != nil {
		s.Log.WarnContext(ctx, "stream: touch", "err", err)
	}
	defer func() {
		touchCtx, done := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer done()
		if touchErr := s.Access.Touch(touchCtx, p.AccountID); touchErr != nil {
			s.Log.WarnContext(touchCtx, "stream: touch", "err", touchErr)
		}
	}()
	expiry := time.NewTimer(p.ExpiresAt.Sub(s.Now()))
	defer expiry.Stop()
	tick := time.NewTicker(s.Tick)
	defer tick.Stop()
	typed := map[uuid.UUID]time.Time{}
	peers := map[uuid.UUID]peer{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.Hub.Done():
			_ = c.Close(websocket.StatusGoingAway, "server shutting down")
			return
		case <-expiry.C:
			_ = c.Close(CloseToken, "token_expired")
			return
		case <-tick.C:
			if err = s.Access.Touch(ctx, p.AccountID); err != nil && ctx.Err() == nil {
				s.Log.WarnContext(ctx, "stream: touch", "err", err)
			}
			// A ring lost without a sign, as on a LISTEN connection that died
			// in silence, costs at most one tick.
			if !catchUp() {
				return
			}
		case <-sub.Behind():
			if !catchUp() {
				return
			}
		case ev := <-sub.Events():
			switch {
			case ev.Seq == 0:
				if !write(typingFrame{Type: KindTyping, ConversationID: ev.Hint.ConversationID}) {
					return
				}
			case ev.Seq <= last:
				// A catch-up sent it already.
			case ev.Seq > last+1:
				// A hole: the log holds the updates between, and this one.
				if !catchUp() {
					return
				}
			default:
				if !write(updateFrame{Type: "update", Seq: ev.Seq, Hint: ev.Hint}) {
					return
				}
				last = ev.Seq
			}
		case f := <-frames:
			switch f.Type {
			case KindTyping:
				now := s.Now()
				if f.ConversationID == nil || now.Sub(typed[*f.ConversationID]) < typingEvery {
					continue
				}
				conv := *f.ConversationID
				if len(typed) >= maxPeers || len(peers) >= maxPeers {
					clear(typed)
					clear(peers)
				}
				typed[conv] = now
				pr, cached := peers[conv]
				if !cached || now.Sub(pr.at) >= peerFor {
					to, allowed, peerErr := s.Access.Peer(ctx, p.AccountID, conv)
					if peerErr != nil {
						s.Log.WarnContext(ctx, "stream: find whom typing reaches", "err", peerErr)
						continue
					}
					pr = peer{to: to, ok: allowed, at: now}
					peers[conv] = pr
				}
				if pr.ok {
					if err = s.Hub.Typing(ctx, pr.to, conv); err != nil {
						s.Log.WarnContext(ctx, "stream: ring typing", "err", err)
					}
				}
			case "auth":
				next, verifyErr := s.Verify(f.Token, s.Now())
				if verifyErr != nil || next.AccountID != p.AccountID {
					_ = c.Close(CloseToken, "invalid_token")
					return
				}
				if suspended, err = s.Access.Suspended(ctx, p.AccountID); err != nil {
					s.Log.WarnContext(ctx, "stream: check suspension", "err", err)
					return
				}
				if suspended {
					_ = c.Close(CloseSuspended, "account_suspended")
					return
				}
				expiry.Reset(next.ExpiresAt.Sub(s.Now()))
			default:
				_ = c.Close(websocket.StatusPolicyViolation, "unknown frame type")
				return
			}
		}
	}
}

// UpdatesHandler serves GET /v1/updates: the caller's log after since, as
// the socket sends it on connect, for an app with no socket open.
func UpdatesHandler(updates UpdateLog, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		var since int64
		if q := r.URL.Query().Get("since"); q != "" {
			v, err := strconv.ParseInt(q, 10, 64)
			if err != nil {
				httpx.WriteError(w, r, log, fmt.Errorf("parse since: %w", err))
				return
			}
			since = v
		}
		page, err := updates.Since(r.Context(), p.AccountID, since, pageSize)
		if err != nil {
			httpx.WriteError(w, r, log, fmt.Errorf("read the update log: %w", err))
			return
		}
		httpx.Respond(w, http.StatusOK, page)
	})
}
