package service_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/testenv"
)

// runRequest is what the fake agent reads of POST /runs.
type runRequest struct {
	TurnID      uuid.UUID         `json:"turn_id"`
	Mode        string            `json:"mode"`
	Question    string            `json:"question"`
	Member      authnMember       `json:"member"`
	AgentConfig *runConfig        `json:"agent_config"`
	History     []json.RawMessage `json:"history"`
	State       json.RawMessage   `json:"state"`
	Steers      []struct {
		SteerID uuid.UUID `json:"steer_id"`
		Text    string    `json:"text"`
	} `json:"steers"`
	Answers      string            `json:"answers"`
	Documents    []json.RawMessage `json:"documents"`
	ControlAfter int64             `json:"control_after"`
}

type runConfig struct {
	Alias string `json:"alias"`
}

type authnMember struct {
	ID   uuid.UUID `json:"id"`
	Role string    `json:"role"`
}

// fakeAgent serves POST /runs with run, after checking the token, and
// keeps every request.
type fakeAgent struct {
	mu   sync.Mutex
	reqs []runRequest
	run  func(req runRequest, w http.ResponseWriter)
}

func (f *fakeAgent) requests() []runRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runRequest(nil), f.reqs...)
}

// lines writes each line as NDJSON and flushes it.
func lines(w http.ResponseWriter, ls ...string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	for _, l := range ls {
		_, _ = io.WriteString(w, l+"\n")
		w.(http.Flusher).Flush()
	}
}

// succeeded is an end line of a run that answered, citing one source.
const succeeded = `{"kind":"end","status":"SUCCEEDED","error_code":null,"answer":"Đã đọc [1].",` +
	`"blocks":[{"type":"text","markdown":"Đã đọc [1]."}],` +
	`"sources":[{"n":1,"kind":"ticket","record_id":"t1","label":"Ticket","call_id":"0193e000-0000-7000-8000-000000000c01","tool":"ticket_get"}],` +
	`"usage":{"input_tokens":10,"output_tokens":5,"cache_read_tokens":2,"cache_write_tokens":0,"cost_usd":0.001,"first_token_ms":300,"provider":"anthropic"},` +
	`"latency_ms":4200,"wrote":true,"state":{"v":1,"wrote":true,"sources":[]},` +
	`"event":{"type":"turn.finished","body":{"status":"SUCCEEDED","outcome":null,"continuable":false,"sources":[]}}}`

// runsOn builds the service on a migrated database and the fake agent,
// with a moderator who asks.
func runsOn(t *testing.T, f *fakeAgent) (*service.Agent, *pgxpool.Pool, authn.ConsoleMember) {
	t.Helper()
	db := pgtest.New(t)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/runs" || r.Header.Get("Authorization") != "Bearer service-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req runRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, req)
		run := f.run
		f.mu.Unlock()
		run(req, w)
	}))
	t.Cleanup(agent.Close)
	s := service.New(service.Deps{
		Settings: repository.NewSettings(db), Runs: repository.NewRuns(db, slog.New(slog.DiscardHandler)),
		BaseURL: agent.URL, Token: "service-token", HTTP: &http.Client{}, Now: time.Now, Log: slog.New(slog.DiscardHandler),
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.FollowTurns(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return s, db, newMember(t, db, "moderator")
}

// newMember puts a console member with role in the database.
func newMember(t *testing.T, db *pgxpool.Pool, role string) authn.ConsoleMember {
	t.Helper()
	m := authn.ConsoleMember{MemberID: uuid.NewV7(), Role: role}
	if _, err := db.Exec(context.Background(), `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, 'Lan', @role, 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": m.MemberID, "email": m.MemberID.String() + "@console.test", "role": role, "at": time.Now()}); err != nil {
		t.Fatal(err)
	}
	return m
}

// waitFor waits until the turn has status.
func waitFor(t *testing.T, s *service.Agent, caller authn.ConsoleMember, turn uuid.UUID, status string) service.Turn {
	t.Helper()
	var v service.Turn
	testenv.Eventually(t, 5*time.Second, func() error {
		var err error
		if v, err = s.Turn(context.Background(), caller, turn); err != nil {
			return err
		}
		if v.Status != status {
			return fmt.Errorf("status %s, want %s", v.Status, status)
		}
		return nil
	})
	return v
}

// The question opens a running turn and goes to the agent with the member,
// the Console assistant's configuration and the cleaned text; each line
// the agent streams back lands in the turn, its tool calls, its document
// and its events. UC-MOD-08.
func TestAskRunsTheTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	call, doc := uuid.NewV7(), uuid.NewV7()
	f := &fakeAgent{run: func(_ runRequest, w http.ResponseWriter) {
		lines(w,
			`{"kind":"event","type":"turn.started","body":{"agent":{"id":"a","name":"Console assistant"},"model":"console-agent","deadline_s":60}}`,
			`{"kind":"checkpoint","state":{"v":1,"wrote":false}}`,
			`{"kind":"event","type":"step.started","body":{"step_id":"s1","title":"Đọc ticket"}}`,
			fmt.Sprintf(`{"kind":"tool_call","id":%q,"tool_use_id":"tu1","tool":"ticket_get","label":"Ticket","source":"mcp",`+
				`"input":{"id":"x"},"status":"running","output":null,"content":null,"output_spilled":false,"latency_ms":null,`+
				`"attempts":null,"attempt_errors":null,"timing":null,"finished_at":null}`, call),
			fmt.Sprintf(`{"kind":"tool_call","id":%q,"tool_use_id":"tu1","tool":"ticket_get","label":"Ticket","source":"mcp",`+
				`"input":{"id":"x"},"status":"success","output":"{\"status\":\"open\"}","content":[{"text":"open"}],`+
				`"output_spilled":false,"latency_ms":120,"attempts":2,"attempt_errors":[{"attempt":1,"error":"timeout","at":"2026-11-19T10:00:00Z"}],`+
				`"timing":{"queued":1,"server":100,"db":null,"return":4},"finished_at":"2026-11-19T10:00:01Z"}`, call),
			fmt.Sprintf(`{"kind":"tool_call","id":%q,"tool_use_id":"tu1","tool":"ticket_get","label":"Ticket","source":"mcp",`+
				`"input":{"id":"x"},"status":"running","output":null,"content":null,"output_spilled":false,"latency_ms":null,`+
				`"attempts":null,"attempt_errors":null,"timing":null,"finished_at":null}`, call),
			fmt.Sprintf(`{"kind":"document","document_id":%q,"version":1,"title":"Báo cáo","sections":[{"type":"markdown","markdown":"# A"}]}`, doc),
			succeeded,
		)
	}}
	s, _, lan := runsOn(t, f)
	c, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "Ai mở <b>ticket</b>?"})
	if err != nil {
		t.Fatal(err)
	}
	v := waitFor(t, s, lan, started.TurnID, "succeeded")
	reqs := f.requests()
	if len(reqs) != 1 || reqs[0].Mode != "new" || reqs[0].Question != "Ai mở ticket?" || reqs[0].Member.ID != lan.MemberID ||
		reqs[0].Member.Role != "moderator" || reqs[0].AgentConfig == nil || reqs[0].AgentConfig.Alias != "console-agent" ||
		reqs[0].History == nil || reqs[0].ControlAfter != 0 || reqs[0].TurnID != started.TurnID {
		t.Errorf("the run request %+v", reqs)
	}
	if v.Answer != "Đã đọc [1]." || len(v.Blocks) != 1 || len(v.Sources) != 1 || v.Sources[0].N != 1 || v.Usage.InputTokens != 10 ||
		v.LastSeq != 3 || v.FinishedAt == nil || *v.DurationMS != 4200 || v.Outcome != "" || v.Continuable || v.Text != "Ai mở ticket?" {
		t.Errorf("the turn %+v", v)
	}
	tc, err := s.ToolCall(ctx, lan, call)
	if err != nil || tc.Status != "success" || string(tc.Output) != `{"status":"open"}` || tc.Attempts != 2 || len(tc.AttemptErrors) != 1 ||
		tc.Timing == nil || *tc.Timing.Server != 100 || *tc.DurationMS != 120 {
		t.Errorf("the tool call %+v %v", tc, err)
	}
	d, err := s.Document(ctx, lan, doc)
	if err != nil || d.LatestVersion != 1 || len(d.Versions) != 1 || d.ConversationID != c.ID {
		t.Errorf("the document %+v %v", d, err)
	}
	detail, err := s.Conversation(ctx, lan, c.ID)
	if err != nil || detail.Title != "Ai mở ticket?" || len(detail.Turns) != 1 {
		t.Errorf("the conversation %+v %v", detail, err)
	}
	other := authn.ConsoleMember{MemberID: uuid.NewV7(), Role: "moderator"}
	if _, err = s.Turn(ctx, other, started.TurnID); !errors.Is(err, domain.ErrTurnNotFound) {
		t.Errorf("another member's turn: %v", err)
	}
	if _, err = s.ToolCall(ctx, other, call); !errors.Is(err, domain.ErrToolCallNotFound) {
		t.Errorf("another member's tool call: %v", err)
	}

	// A second question replays the first.
	if _, err = s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "Còn gì nữa?"}); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, 5*time.Second, func() error {
		if reqs = f.requests(); len(reqs) != 2 {
			return errors.New("no second run")
		}
		return nil
	})
	var h []struct {
		Status  string `json:"status"`
		Answer  string `json:"answer"`
		Entries []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"entries"`
	}
	b, _ := json.Marshal(reqs[1].History)
	if err = json.Unmarshal(b, &h); err != nil || len(h) != 1 || h[0].Status != "SUCCEEDED" || h[0].Answer != "Đã đọc [1]." ||
		len(h[0].Entries) != 1 || h[0].Entries[0].Kind != "tool" || h[0].Entries[0].Status != "success" {
		t.Errorf("the history %s", b)
	}
}

// A refused run, a busy conversation and the input guard reach the member
// as their codes, and a refused run leaves no turn. UC-MOD-08.
func TestAskRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var refuse bool
	var mu sync.Mutex
	f := &fakeAgent{run: func(_ runRequest, w http.ResponseWriter) {
		mu.Lock()
		no := refuse
		mu.Unlock()
		if no {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"code":"agent_unavailable"}`)
			return
		}
		lines(w, `{"kind":"end","status":"PAUSED","error_code":null,"answer":null,"blocks":[],"sources":[],`+
			`"usage":{"input_tokens":0,"output_tokens":0,"cache_read_tokens":0,"cache_write_tokens":0,"cost_usd":0,"first_token_ms":null,"provider":null},`+
			`"latency_ms":10,"wrote":true,"state":{"v":1,"wrote":true},"event":{"type":"turn.paused","body":{"steps_done":0,"reason":"member"}}}`)
	}}
	s, _, lan := runsOn(t, f)
	c, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	refuse = true
	mu.Unlock()
	if _, err = s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("a refused run: %v", err)
	}
	if detail, _ := s.Conversation(ctx, lan, c.ID); len(detail.Turns) != 0 {
		t.Errorf("a refused run left %+v", detail.Turns)
	}
	mu.Lock()
	refuse = false
	mu.Unlock()
	started, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, lan, started.TurnID, "paused")
	if _, err = s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrTurnInProgress) {
		t.Errorf("a paused turn: %v", err)
	}
	if err = s.DeleteConversation(ctx, lan, c.ID); !errors.Is(err, domain.ErrTurnInProgress) {
		t.Errorf("deleting with a turn open: %v", err)
	}
	if _, err = s.Ask(ctx, lan, c.ID, service.NewTurn{Text: strings.Repeat("a", 2001)}); !errors.Is(err, domain.ErrInputRejected) {
		t.Errorf("a long question: %v", err)
	}
	if _, err = s.Ask(ctx, lan, c.ID, service.NewTurn{Text: " <br> "}); !errors.Is(err, domain.ErrRequestInvalid) {
		t.Errorf("an empty question: %v", err)
	}
	if _, err = s.Ask(ctx, lan, uuid.NewV7(), service.NewTurn{Text: "?"}); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("an unknown conversation: %v", err)
	}
	app := uuid.MustParse("0193e000-0000-7000-8000-00000000a102")
	if _, err = s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?", AgentID: &app}); !errors.Is(err, domain.ErrAgentNotFound) {
		t.Errorf("an app agent: %v", err)
	}
}

// A stream that breaks before its end closes the turn as agent_stuck; a
// turn that wrote something is continued from its checkpoint, with
// turn.resumed. UC-MOD-08 A1.
func TestBrokenRunContinues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := &fakeAgent{run: func(req runRequest, w http.ResponseWriter) {
		if req.Mode == "new" {
			lines(w, `{"kind":"checkpoint","state":{"v":1,"wrote":false,"question":"?"}}`,
				`{"kind":"event","type":"step.started","body":{"step_id":"s1","title":"Đọc"}}`)
			return
		}
		lines(w, succeeded)
	}}
	s, _, lan := runsOn(t, f)
	c, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?"})
	if err != nil {
		t.Fatal(err)
	}
	v := waitFor(t, s, lan, started.TurnID, "failed")
	if v.ErrorCode != domain.FailStuck || v.Outcome != "broken" || !v.Continuable || v.Answer != domain.FailureAnswers[domain.FailStuck] {
		t.Fatalf("a broken run %+v", v)
	}
	if _, err = s.Control(ctx, lan, started.TurnID, service.ControlPause); !errors.Is(err, domain.ErrTurnState) {
		t.Errorf("pausing a failed turn: %v", err)
	}
	if _, err = s.Control(ctx, lan, started.TurnID, service.ControlContinue); err != nil {
		t.Fatal(err)
	}
	v = waitFor(t, s, lan, started.TurnID, "succeeded")
	reqs := f.requests()
	if len(reqs) != 2 || reqs[1].Mode != "resume" || !strings.Contains(string(reqs[1].State), `"question":"?"`) ||
		reqs[1].AgentConfig == nil || reqs[1].Steers == nil {
		t.Errorf("the resume %+v %s", reqs, reqs[len(reqs)-1].State)
	}
	if v.ErrorCode != "" || v.Answer != "Đã đọc [1]." {
		t.Errorf("the continued turn %+v", v)
	}
	body, err := s.Events(ctx, lan, started.TurnID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	all, _ := io.ReadAll(body)
	for _, want := range []string{"event: step.started", `"failure":"agent_stuck"`, `"by":"continue"`, `"status":"SUCCEEDED"`} {
		if !strings.Contains(string(all), want) {
			t.Errorf("the events lack %s: %s", want, all)
		}
	}
}

// Pause, stop and a steer to a running turn wait in its control queue,
// which the agent polls; a steer.read marks the steer read; the stream
// follows the run live and ends at turn.finished. UC-MOD-08.
func TestControlQueueAndLiveStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	release := make(chan string)
	f := &fakeAgent{run: func(_ runRequest, w http.ResponseWriter) {
		lines(w, `{"kind":"event","type":"turn.started","body":{}}`)
		steer := <-release
		lines(w, `{"kind":"event","type":"steer.read","body":{"steer_id":"`+steer+`","step_id":null,"step_number":1}}`, succeeded)
	}}
	s, _, lan := runsOn(t, f)
	c, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := s.Events(ctx, lan, started.TurnID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	events := bufio.NewReader(body)
	readUntil := func(want string) {
		t.Helper()
		for {
			line, readErr := events.ReadString('\n')
			if readErr != nil {
				t.Fatalf("no %s: %v", want, readErr)
			}
			if strings.HasPrefix(line, want) {
				return
			}
		}
	}
	readUntil("event: turn.started")
	if _, err = s.Control(ctx, lan, started.TurnID, service.ControlPause); err != nil {
		t.Fatal(err)
	}
	steer, err := s.Steer(ctx, lan, started.TurnID, "Chỉ Hà Nội")
	if err != nil || steer.Resumed {
		t.Fatalf("steer %+v %v", steer, err)
	}
	queue, err := s.TurnControl(ctx, started.TurnID, 0)
	if err != nil || !queue.Running || len(queue.Items) != 2 || queue.Items[0].Kind != "pause" || queue.Items[1].Kind != "steer" ||
		*queue.Items[1].SteerID != steer.SteerID || queue.Items[1].Text != "Chỉ Hà Nội" {
		t.Fatalf("the queue %+v %v", queue, err)
	}
	if after, _ := s.TurnControl(ctx, started.TurnID, queue.Items[1].Seq); len(after.Items) != 0 {
		t.Errorf("after the last seq %+v", after)
	}
	release <- steer.SteerID.String()
	readUntil("event: steer.read")
	readUntil("event: turn.finished")
	if _, err = events.ReadString('\n'); err != nil && !errors.Is(err, io.EOF) {
		t.Errorf("after turn.finished: %v", err)
	}
	v := waitFor(t, s, lan, started.TurnID, "succeeded")
	if len(v.Steers) != 1 || v.Steers[0].SteerID != steer.SteerID {
		t.Errorf("the steers %+v", v.Steers)
	}
	if gone, _ := s.TurnControl(ctx, started.TurnID, 0); gone.Running {
		t.Error("a finished turn runs")
	}
	if gone, _ := s.TurnControl(ctx, uuid.NewV7(), 0); gone.Running || gone.Items == nil {
		t.Errorf("an unknown turn %+v", gone)
	}
}

// A turn waiting on questions takes answers that fit them and goes on with
// their text, or ends cancelled. UC-MOD-08.
func TestAnswersAndEnd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ask := `{"kind":"end","status":"AWAITING_INPUT","error_code":null,"answer":null,"blocks":[],"sources":[],` +
		`"usage":{"input_tokens":1,"output_tokens":1,"cache_read_tokens":0,"cache_write_tokens":0,"cost_usd":0,"first_token_ms":null,"provider":null},` +
		`"latency_ms":10,"wrote":true,"state":{"v":1,"wrote":true,"sources":[],"pending_ask":[{"id":"range","label":"Khoảng","kind":"single",` +
		`"options":[{"id":"week","label":"Tuần này"}]}]},"event":{"type":"clarify","body":{"questions":[]}}}`
	f := &fakeAgent{run: func(_ runRequest, w http.ResponseWriter) { lines(w, ask) }}
	s, _, lan := runsOn(t, f)
	c, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Ask(ctx, lan, c.ID, service.NewTurn{Text: "?"})
	if err != nil {
		t.Fatal(err)
	}
	v := waitFor(t, s, lan, started.TurnID, "awaiting_input")
	if len(v.Questions) != 1 {
		t.Fatalf("the questions %+v", v)
	}
	if _, err = s.Answer(ctx, lan, started.TurnID, service.Answers{"range": json.RawMessage(`"year"`)}); !errors.Is(err, domain.ErrAnswersInvalid) {
		t.Errorf("an answer that does not fit: %v", err)
	}
	if _, err = s.Answer(ctx, lan, started.TurnID, service.Answers{"range": json.RawMessage(`"week"`)}); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, 5*time.Second, func() error {
		reqs := f.requests()
		if len(reqs) != 2 || reqs[1].Answers != "The member's answers to your questions:\n- Khoảng: Tuần này" || strings.Contains(string(reqs[1].State), "pending_ask") {
			return fmt.Errorf("the resume %+v", reqs)
		}
		return nil
	})
	waitFor(t, s, lan, started.TurnID, "awaiting_input")
	end, err := s.Control(ctx, lan, started.TurnID, service.ControlEnd)
	if err != nil || end.Status != "cancelled" {
		t.Fatalf("end %+v %v", end, err)
	}
	v = waitFor(t, s, lan, started.TurnID, "cancelled")
	if v.Outcome != "stopped" || v.Continuable || v.Questions != nil {
		t.Errorf("the ended turn %+v", v)
	}
	if _, err = s.Control(ctx, lan, started.TurnID, service.ControlEnd); !errors.Is(err, domain.ErrTurnState) {
		t.Errorf("ending again: %v", err)
	}
}

// The sweep closes a turn running past its deadline as agent_stuck.
// UC-MOD-08.
func TestSweepTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db, lan := runsOn(t, &fakeAgent{})
	c, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	turn := uuid.NewV7()
	if _, err = db.Exec(ctx, `INSERT INTO agent_turns (id, conversation_id, member_id, ordinal, status, question, started_at,
    run_started_at, deadline_ms, wrote)
VALUES (@id, @c, @m, 0, 'running', '?', now() - interval '5 minutes', now() - interval '5 minutes', 60000, true)`,
		pgx.StrictNamedArgs{"id": turn, "c": c.ID, "m": lan.MemberID}); err != nil {
		t.Fatal(err)
	}
	if err = s.SweepTurns(ctx); err != nil {
		t.Fatal(err)
	}
	v, err := s.Turn(ctx, lan, turn)
	if err != nil || v.Status != "failed" || v.ErrorCode != domain.FailStuck || !v.Continuable || v.LastSeq != 1 {
		t.Errorf("the swept turn %+v %v", v, err)
	}
}

// A record's conversation is shared by every member and made once; one
// without a record is its member's own, which only they and an admin
// change; a shared one only an admin. UC-MOD-08.
func TestConversations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db, lan := runsOn(t, &fakeAgent{})
	minh := newMember(t, db, "admin")
	subject := uuid.NewV7()
	shared, err := s.CreateConversation(ctx, lan, service.NewConversation{SubjectKind: domain.SubjectTicket, SubjectID: &subject})
	if err != nil || shared.SubjectKind != domain.SubjectTicket || *shared.SubjectID != subject {
		t.Fatalf("the record's conversation %+v %v", shared, err)
	}
	if again, _ := s.CreateConversation(ctx, minh, service.NewConversation{SubjectKind: domain.SubjectTicket, SubjectID: &subject}); again.ID != shared.ID {
		t.Errorf("a second conversation for the record %s", again.ID)
	}
	if _, err = s.CreateConversation(ctx, lan, service.NewConversation{SubjectKind: domain.SubjectAccount}); !errors.Is(err, domain.ErrInvalidSubject) {
		t.Errorf("a kind alone: %v", err)
	}
	if _, err = s.Conversations(ctx, lan, "", &subject); !errors.Is(err, domain.ErrInvalidSubject) {
		t.Errorf("an id alone: %v", err)
	}
	own, err := s.CreateConversation(ctx, lan, service.NewConversation{})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Conversations(ctx, lan, "", nil); len(list) != 1 || list[0].ID != own.ID {
		t.Errorf("lan's own %+v", list)
	}
	if list, _ := s.Conversations(ctx, minh, domain.SubjectTicket, &subject); len(list) != 1 || list[0].ID != shared.ID {
		t.Errorf("by subject %+v", list)
	}
	if _, err = s.Conversation(ctx, minh, own.ID); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("an admin reads another member's own: %v", err)
	}
	if _, err = s.RenameConversation(ctx, lan, shared.ID, "Ticket"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("a moderator renames a shared one: %v", err)
	}
	if c, renameErr := s.RenameConversation(ctx, lan, own.ID, "  Ring check  "); renameErr != nil || c.Title != "Ring check" {
		t.Errorf("rename %+v %v", c, renameErr)
	}
	if _, err = s.RenameConversation(ctx, lan, own.ID, "   "); !errors.Is(err, domain.ErrTitleInvalid) {
		t.Errorf("an empty title: %v", err)
	}
	other := newMember(t, db, "moderator")
	if err = s.DeleteConversation(ctx, other, own.ID); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("another member deletes lan's own: %v", err)
	}
	if err = s.DeleteConversation(ctx, minh, shared.ID); err != nil {
		t.Errorf("an admin deletes a shared one: %v", err)
	}
	if _, err = s.Conversation(ctx, lan, shared.ID); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Errorf("the deleted conversation: %v", err)
	}
}

// Every member shares the skills; only the creator or an admin changes
// one, and a slug is taken once. UC-MOD-08.
func TestSkills(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db, lan := runsOn(t, &fakeAgent{})
	other := newMember(t, db, "moderator")
	sk, err := s.CreateSkill(ctx, lan, service.SkillInput{
		Name: " Báo cáo tuần ", Prompt: "Report on {{week}}",
		Variables: []service.SkillVariable{{Name: "week"}},
	})
	if err != nil || sk.Slug != "bao-cao-tuan" || sk.Name != "Báo cáo tuần" || sk.CreatedBy != lan.MemberID || len(sk.Variables) != 1 {
		t.Fatalf("create %+v %v", sk, err)
	}
	if _, err = s.CreateSkill(ctx, other, service.SkillInput{Name: "x", Slug: "bao-cao-tuan", Prompt: "?"}); !errors.Is(err, domain.ErrSkillTaken) {
		t.Errorf("a slug taken: %v", err)
	}
	if _, err = s.CreateSkill(ctx, other, service.SkillInput{Name: "x", Prompt: "  "}); !errors.Is(err, domain.ErrSkillInvalid) {
		t.Errorf("an empty prompt: %v", err)
	}
	name := "Weekly"
	if _, err = s.UpdateSkill(ctx, other, sk.ID, service.SkillPatch{Name: &name}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("another member's skill: %v", err)
	}
	if got, updateErr := s.UpdateSkill(ctx, lan, sk.ID, service.SkillPatch{Name: &name}); updateErr != nil || got.Name != name || got.Slug != "bao-cao-tuan" {
		t.Errorf("update %+v %v", got, updateErr)
	}
	if list, _ := s.Skills(ctx, other); len(list) != 1 {
		t.Errorf("skills %+v", list)
	}
	if err = s.DeleteSkill(ctx, other, sk.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("deleting another member's skill: %v", err)
	}
	if err = s.DeleteSkill(ctx, lan, sk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Skill(ctx, lan, sk.ID); !errors.Is(err, domain.ErrSkillNotFound) {
		t.Errorf("the deleted skill: %v", err)
	}
}
