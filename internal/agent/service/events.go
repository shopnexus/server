package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
)

const (
	// eventPage is the most events one read of the table returns.
	eventPage = 500
	// keepAlive is the quiet time after which a stream sends a comment and
	// reads the table, and idleClose how long a turn that stopped running
	// may stay silent before its stream ends.
	keepAlive = 15 * time.Second
	idleClose = 10 * time.Second
)

// stopEvents end a stream: the run stopped writing until someone acts.
var stopEvents = map[string]bool{"turn.finished": true, "turn.paused": true, "clarify": true}

// Events is UC-MOD-08 step 3 as the member watches it: a turn's events as
// text/event-stream, replayed after lastEventID (a seq) and then followed
// as agent_turn rings. The caller copies the body and closes it, which
// ends the stream and not the run.
func (s *Agent) Events(ctx context.Context, caller authn.ConsoleMember, turn uuid.UUID, lastEventID string) (io.ReadCloser, error) {
	var after int64
	if lastEventID != "" {
		n, err := strconv.ParseInt(lastEventID, 10, 64)
		if err != nil || n < 0 {
			return nil, domain.ErrRequestInvalid.WithField("last_event_id", apperr.Invalid, "not_a_seq")
		}
		after = n
	}
	if _, err := s.visibleTurn(ctx, caller, turn); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	ring, leave := s.hub.subscribe(turn)
	r, w := io.Pipe()
	go func() {
		defer leave()
		w.CloseWithError(s.follow(ctx, turn, after, ring, w))
	}()
	return stream{ReadCloser: r, cancel: cancel}, nil
}

// stream is an open event stream; Close ends it.
type stream struct {
	io.ReadCloser

	cancel context.CancelFunc
}

func (s stream) Close() error {
	s.cancel()
	return s.ReadCloser.Close() //nolint:wrapcheck // Close of the pipe, as an io.ReadCloser passes it on.
}

// follow writes turn's events after seq after to w until a stop event, or
// until the turn stopped running and stayed silent idleClose, or ctx ends.
func (s *Agent) follow(ctx context.Context, turn uuid.UUID, after int64, ring <-chan struct{}, w io.Writer) error {
	last, lastType := after, ""
	deliver := func() (int, error) {
		n := 0
		for {
			events, err := s.runs.Events(ctx, turn, last, eventPage)
			if err != nil {
				return n, fmt.Errorf("read the turn's events: %w", err)
			}
			for _, e := range events {
				if _, err = w.Write(frame(e)); err != nil {
					return n, err //nolint:wrapcheck // The reader went away.
				}
				last, lastType = e.Seq, e.Type
				n++
			}
			if len(events) < eventPage {
				return n, nil
			}
		}
	}
	running := func() bool {
		t, err := s.runs.Turn(ctx, turn)
		return err == nil && t.Status == domain.TurnRunning
	}
	if _, err := io.WriteString(w, "retry: 2000\n\n"); err != nil {
		return nil
	}
	if _, err := deliver(); err != nil {
		return err
	}
	if lastType == "" && last > 0 {
		if prev, err := s.runs.Events(ctx, turn, last-1, 1); err == nil && len(prev) == 1 {
			lastType = prev[0].Type
		}
	}
	if stopEvents[lastType] && !running() {
		return nil
	}
	timer := time.NewTimer(keepAlive)
	defer timer.Stop()
	var idleSince time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ring:
			n, err := deliver()
			if err != nil {
				return err
			}
			if n > 0 {
				if stopEvents[lastType] {
					return nil
				}
				idleSince = time.Time{}
			}
		case <-timer.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return nil
			}
			n, err := deliver()
			if err != nil {
				return err
			}
			switch {
			case n > 0 && stopEvents[lastType]:
				return nil
			case n > 0 || running():
				idleSince = time.Time{}
			case idleSince.IsZero():
				idleSince = time.Now()
			case time.Since(idleSince) >= idleClose:
				return nil
			default:
			}
			timer.Reset(keepAlive)
		}
	}
}

// frame is one event as a server-sent event: id is the seq, data is
// {seq, type, at, ...body}.
func frame(e repository.Event) []byte {
	head, _ := json.Marshal(struct {
		Seq  int64  `json:"seq"`
		Type string `json:"type"`
		At   string `json:"at"`
	}{e.Seq, e.Type, e.At.UTC().Format("2006-01-02T15:04:05.000Z07:00")})
	data := head[:len(head)-1]
	var compact bytes.Buffer
	if json.Compact(&compact, e.Body) != nil {
		compact.Reset()
	}
	if body := compact.Bytes(); len(body) > 2 && body[0] == '{' {
		if inner := bytes.TrimSpace(body[1 : len(body)-1]); len(inner) > 0 {
			data = append(append(data, ','), inner...)
		}
	}
	data = append(data, '}')
	return fmt.Appendf(nil, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, data)
}

// hub hands the agent_turn rings this replica hears to its open streams
// of each turn. A ring only says the turn has something new; a stream
// reads the table.
type hub struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[chan struct{}]struct{}
}

func newHub() *hub { return &hub{subs: map[uuid.UUID]map[chan struct{}]struct{}{}} }

// subscribe returns a channel that turn's rings reach, and the function
// that stops them.
func (h *hub) subscribe(turn uuid.UUID) (ring <-chan struct{}, leave func()) {
	c := make(chan struct{}, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[turn] == nil {
		h.subs[turn] = map[chan struct{}]struct{}{}
	}
	h.subs[turn][c] = struct{}{}
	return c, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[turn], c)
		if len(h.subs[turn]) == 0 {
			delete(h.subs, turn)
		}
	}
}

func (h *hub) ring(turn uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs[turn] {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

// ringAll rings every stream, after rings may have been lost.
func (h *hub) ringAll() {
	h.mu.Lock()
	turns := make([]uuid.UUID, 0, len(h.subs))
	for t := range h.subs {
		turns = append(turns, t)
	}
	h.mu.Unlock()
	for _, t := range turns {
		h.ring(t)
	}
}

// FollowTurns hands this replica's streams the agent_turn rings until ctx
// is done.
func (s *Agent) FollowTurns(ctx context.Context) {
	s.runs.ListenTurns(ctx, s.hub.ring, s.hub.ringAll)
}
