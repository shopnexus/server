package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/postgres"
)

// turnChannel is the channel a turn's new event rings on, its id the
// payload.
const turnChannel = "agent_turn"

type pgConversations struct {
	db  *pgxpool.Pool
	log *slog.Logger
}

// NewConversations keeps the console agent's conversations on db; log is
// for ListenTurns.
// inTx runs fn in a transaction, what naming it in an error.
func inTx(ctx context.Context, db *pgxpool.Pool, what string, fn func(pgx.Tx) error) error {
	if err := pgx.BeginFunc(ctx, db, fn); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// scanned is a row's scan error, wrapped.
func scanned(err error) error {
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	return nil
}

func NewConversations(db *pgxpool.Pool, log *slog.Logger) Conversations {
	return pgConversations{db: db, log: log}
}

// uniqueOn names the unique index or constraint a write broke, or "".
func uniqueOn(err error) string {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}

const conversationColumns = `id, member_id, COALESCE(subject_kind, ''), subject_id, COALESCE(title, ''), created_at,
    updated_at, last_activity_at`

func scanConversation(r pgx.CollectableRow) (Conversation, error) {
	var c Conversation
	if err := r.Scan(&c.ID, &c.MemberID, &c.SubjectKind, &c.SubjectID, &c.Title, &c.CreatedAt, &c.UpdatedAt,
		&c.LastActivityAt); err != nil {
		return c, fmt.Errorf("scan agent conversation: %w", err)
	}
	return c, nil
}

func oneConversation(rows pgx.Rows, err error) (Conversation, error) {
	if err != nil {
		return Conversation{}, fmt.Errorf("select agent conversation: %w", err)
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanConversation)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("read agent conversation: %w", err)
	}
	return c, nil
}

func (s pgConversations) OpenConversation(ctx context.Context, c Conversation) (Conversation, error) {
	var kind *string
	key := c.RequestKey
	if c.SubjectKind != "" {
		kind, key = &c.SubjectKind, nil
	}
	// The insert claims the subject or the member's request key; a claimed
	// one inserts nothing and the read below finds its conversation.
	if _, err := s.db.Exec(ctx, `INSERT INTO agent_conversations (id, member_id, subject_kind, subject_id, created_at,
    updated_at, last_activity_at, request_key)
VALUES (@id, @member, @kind, @subject, @at, @at, @at, @key) ON CONFLICT DO NOTHING`, pgx.StrictNamedArgs{
		"id": c.ID, "member": c.MemberID, "kind": kind, "subject": c.SubjectID, "at": c.CreatedAt, "key": key,
	}); err != nil {
		return Conversation{}, fmt.Errorf("insert agent conversation: %w", err)
	}
	if kind == nil {
		return oneConversation(s.db.Query(ctx, `SELECT `+conversationColumns+` FROM agent_conversations
WHERE id = @id OR (member_id = @member AND request_key = @key)`, pgx.StrictNamedArgs{"id": c.ID, "member": c.MemberID, "key": key}))
	}
	return oneConversation(s.db.Query(ctx, `SELECT `+conversationColumns+` FROM agent_conversations
WHERE subject_kind = @kind AND subject_id = @subject`, pgx.StrictNamedArgs{"kind": kind, "subject": c.SubjectID}))
}

func (s pgConversations) Conversations(ctx context.Context, member uuid.UUID, f ConversationFilter, limit int) ([]Conversation, error) {
	rows, err := s.db.Query(ctx, `SELECT `+conversationColumns+` FROM agent_conversations
WHERE CASE WHEN @kind = '' THEN subject_kind IS NULL AND member_id = @member
    ELSE subject_kind = @kind AND (@subject::uuid IS NULL OR subject_id = @subject) END
ORDER BY last_activity_at DESC, id LIMIT @limit`, pgx.StrictNamedArgs{
		"member": member, "kind": f.SubjectKind, "subject": f.SubjectID, "limit": limit,
	})
	if err != nil {
		return nil, fmt.Errorf("select agent conversations: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanConversation)
	if err != nil {
		return nil, fmt.Errorf("read agent conversations: %w", err)
	}
	return out, nil
}

func (s pgConversations) Conversation(ctx context.Context, id uuid.UUID) (Conversation, error) {
	return oneConversation(s.db.Query(ctx, `SELECT `+conversationColumns+` FROM agent_conversations WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}))
}

func (s pgConversations) RenameConversation(ctx context.Context, id uuid.UUID, title string, at time.Time) (Conversation, error) {
	return oneConversation(s.db.Query(ctx, `UPDATE agent_conversations SET title = @title, updated_at = @at WHERE id = @id
RETURNING `+conversationColumns, pgx.StrictNamedArgs{"id": id, "title": title, "at": at}))
}

func (s pgConversations) DeleteConversation(ctx context.Context, id uuid.UUID) error {
	return inTx(ctx, s.db, "delete agent conversation", func(tx pgx.Tx) error {
		var running bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agent_turns WHERE conversation_id = c.id AND status = 'running')
FROM agent_conversations AS c WHERE c.id = @id FOR UPDATE`, pgx.StrictNamedArgs{"id": id}).Scan(&running)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return fmt.Errorf("lock agent conversation: %w", err)
		case running:
			return ErrBusy
		}
		if _, err = tx.Exec(ctx, `DELETE FROM agent_conversations WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
			return fmt.Errorf("delete agent conversation: %w", err)
		}
		return nil
	})
}

func (s pgConversations) Messages(ctx context.Context, conversation uuid.UUID) ([]Message, error) {
	rows, err := s.db.Query(ctx, `SELECT seq, turn_id, message, created_at FROM agent_messages
WHERE conversation_id = @id ORDER BY seq`, pgx.StrictNamedArgs{"id": conversation})
	if err != nil {
		return nil, fmt.Errorf("select agent messages: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) {
		var m Message
		scanErr := r.Scan(&m.Seq, &m.TurnID, &m.Body, &m.At)
		return m, scanned(scanErr)
	})
	if err != nil {
		return nil, fmt.Errorf("read agent messages: %w", err)
	}
	return out, nil
}

const turnColumns = `t.id, t.conversation_id, t.member_id, t.agent_id, t.agent_name, t.model, t.capabilities, t.status,
    COALESCE(t.stop_reason, ''), COALESCE(t.error_text, ''), t.token_hash, t.input_tokens, t.output_tokens,
    t.cached_tokens, t.event_seq, t.request_key, t.started_at, t.run_started_at, t.deadline_at, t.finished_at,
    NOT EXISTS (SELECT 1 FROM agent_turns AS n WHERE n.conversation_id = t.conversation_id AND n.started_at > t.started_at),
    c.member_id, COALESCE(c.subject_kind, ''), c.subject_id`

const turnFrom = ` FROM agent_turns AS t JOIN agent_conversations AS c ON c.id = t.conversation_id `

func scanTurn(r pgx.CollectableRow) (Turn, error) {
	var t Turn
	if err := r.Scan(&t.ID, &t.ConversationID, &t.MemberID, &t.AgentID, &t.AgentName, &t.Model, &t.Capabilities, &t.Status,
		&t.Stop, &t.Error, &t.TokenHash, &t.Usage.InputTokens, &t.Usage.OutputTokens, &t.Usage.CachedTokens, &t.EventSeq,
		&t.RequestKey, &t.StartedAt, &t.RunStartedAt, &t.DeadlineAt, &t.FinishedAt, &t.Latest, &t.Owner, &t.SubjectKind,
		&t.SubjectID); err != nil {
		return t, fmt.Errorf("scan agent turn: %w", err)
	}
	return t, nil
}

func oneTurn(rows pgx.Rows, err error) (Turn, error) {
	if err != nil {
		return Turn{}, fmt.Errorf("select agent turn: %w", err)
	}
	t, err := pgx.CollectExactlyOneRow(rows, scanTurn)
	if errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, ErrNotFound
	}
	if err != nil {
		return Turn{}, fmt.Errorf("read agent turn: %w", err)
	}
	return t, nil
}

func (s pgConversations) Turns(ctx context.Context, conversation uuid.UUID) ([]Turn, error) {
	rows, err := s.db.Query(ctx, `SELECT `+turnColumns+turnFrom+`WHERE t.conversation_id = @id ORDER BY t.started_at`,
		pgx.StrictNamedArgs{"id": conversation})
	if err != nil {
		return nil, fmt.Errorf("select agent turns: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanTurn)
	if err != nil {
		return nil, fmt.Errorf("read agent turns: %w", err)
	}
	return out, nil
}

func (s pgConversations) Turn(ctx context.Context, id uuid.UUID) (Turn, error) {
	return oneTurn(s.db.Query(ctx, `SELECT `+turnColumns+turnFrom+`WHERE t.id = @id`, pgx.StrictNamedArgs{"id": id}))
}

func (s pgConversations) TurnByKey(ctx context.Context, member, key uuid.UUID) (Turn, error) {
	return oneTurn(s.db.Query(ctx, `SELECT `+turnColumns+turnFrom+`WHERE t.member_id = @member AND t.request_key = @key`,
		pgx.StrictNamedArgs{"member": member, "key": key}))
}

func (s pgConversations) TurnByToken(ctx context.Context, hash []byte, at time.Time) (Turn, error) {
	return oneTurn(s.db.Query(ctx, `SELECT `+turnColumns+turnFrom+`
WHERE t.token_hash = @hash AND t.status = 'running' AND t.deadline_at > @at`, pgx.StrictNamedArgs{"hash": hash, "at": at}))
}

// touch marks a conversation active then and gives it title when it has
// none.
func touch(ctx context.Context, tx pgx.Tx, conversation uuid.UUID, title string, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE agent_conversations
SET last_activity_at = @at, updated_at = @at, title = COALESCE(title, NULLIF(@title, ''))
WHERE id = @id`, pgx.StrictNamedArgs{"id": conversation, "title": title, "at": at}); err != nil {
		return fmt.Errorf("touch agent conversation: %w", err)
	}
	return nil
}

func (s pgConversations) OpenTurn(ctx context.Context, t Turn, title string, at time.Time) error {
	return inTx(ctx, s.db, "open agent turn", func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM agent_conversations WHERE id = @id FOR UPDATE`,
			pgx.StrictNamedArgs{"id": t.ConversationID}).Scan(nil); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock agent conversation: %w", err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO agent_turns (id, conversation_id, member_id, agent_id, agent_name, model,
    capabilities, status, token_hash, request_key, started_at, run_started_at, deadline_at)
VALUES (@id, @conversation, @member, @agent, @agent_name, @model, @capabilities, 'running', @hash, @key, @at, @at,
    @deadline)`, pgx.StrictNamedArgs{
			"id": t.ID, "conversation": t.ConversationID, "member": t.MemberID, "agent": t.AgentID,
			"agent_name": t.AgentName, "model": t.Model, "capabilities": t.Capabilities, "hash": t.TokenHash,
			"key": t.RequestKey, "at": t.StartedAt, "deadline": t.DeadlineAt,
		})
		switch uniqueOn(err) {
		case "agent_turns_running":
			return ErrBusy
		case "agent_turns_member_request_key":
			return ErrKeyUsed
		}
		if err != nil {
			return fmt.Errorf("insert agent turn: %w", err)
		}
		return touch(ctx, tx, t.ConversationID, title, at)
	})
}

func (s pgConversations) ResumeTurn(ctx context.Context, res Resume, at time.Time) error {
	return inTx(ctx, s.db, "resume agent turn", func(tx pgx.Tx) error {
		var conversation uuid.UUID
		var status string
		var latest bool
		err := tx.QueryRow(ctx, `SELECT t.conversation_id, t.status,
    NOT EXISTS (SELECT 1 FROM agent_turns AS n WHERE n.conversation_id = t.conversation_id AND n.started_at > t.started_at)
`+turnFrom+`WHERE t.id = @id FOR UPDATE OF c, t`, pgx.StrictNamedArgs{"id": res.TurnID}).Scan(&conversation, &status, &latest)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return fmt.Errorf("lock agent turn: %w", err)
		case status == "running" || !latest:
			return ErrState
		}
		_, err = tx.Exec(ctx, `UPDATE agent_turns SET status = 'running', stop_reason = NULL, error_text = NULL,
    token_hash = @hash, run_started_at = @run, deadline_at = @deadline, finished_at = NULL
WHERE id = @id`, pgx.StrictNamedArgs{"id": res.TurnID, "hash": res.TokenHash, "run": res.RunStartedAt, "deadline": res.DeadlineAt})
		if uniqueOn(err) == "agent_turns_running" {
			return ErrBusy
		}
		if err != nil {
			return fmt.Errorf("resume agent turn: %w", err)
		}
		return touch(ctx, tx, conversation, "", at)
	})
}

func (s pgConversations) DeleteTurn(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM agent_turns WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete agent turn: %w", err)
	}
	return nil
}

func (s pgConversations) ResetTurn(ctx context.Context, before Turn) error {
	if _, err := s.db.Exec(ctx, `UPDATE agent_turns SET status = @status, stop_reason = NULLIF(@stop, ''),
    error_text = NULLIF(@error, ''), token_hash = NULL, run_started_at = @run, deadline_at = @deadline,
    finished_at = @finished
WHERE id = @id`, pgx.StrictNamedArgs{
		"id": before.ID, "status": before.Status, "stop": before.Stop, "error": before.Error, "run": before.RunStartedAt,
		"deadline": before.DeadlineAt, "finished": before.FinishedAt,
	}); err != nil {
		return fmt.Errorf("reset agent turn: %w", err)
	}
	return nil
}

func (s pgConversations) AppendMessages(ctx context.Context, turn uuid.UUID, run time.Time, msgs []json.RawMessage,
	at time.Time,
) (bool, error) {
	if len(msgs) == 0 {
		return true, nil
	}
	appended := false
	err := inTx(ctx, s.db, "append agent messages", func(tx pgx.Tx) error {
		var conversation uuid.UUID
		var last int
		// Locking the conversation orders the writers of its seq.
		err := tx.QueryRow(ctx, `SELECT c.id, COALESCE((SELECT max(seq) FROM agent_messages WHERE conversation_id = c.id), 0)
`+turnFrom+`WHERE t.id = @turn AND t.status = 'running' AND t.run_started_at = @run FOR UPDATE OF c`,
			pgx.StrictNamedArgs{"turn": turn, "run": run}).Scan(&conversation, &last)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock agent conversation: %w", err)
		}
		b := &pgx.Batch{}
		for i, m := range msgs {
			b.Queue(`INSERT INTO agent_messages (conversation_id, seq, turn_id, message, created_at)
VALUES (@conversation, @seq, @turn, @message, @at)`, pgx.StrictNamedArgs{
				"conversation": conversation, "seq": last + i + 1, "turn": turn, "message": m, "at": at,
			}).Fn = postgres.ExecAs("insert agent message", nil)
		}
		b.Queue(`UPDATE agent_conversations SET last_activity_at = @at WHERE id = @id`,
			pgx.StrictNamedArgs{"id": conversation, "at": at}).Fn = postgres.ExecAs("touch agent conversation", nil)
		if err = tx.SendBatch(ctx, b).Close(); err != nil {
			return fmt.Errorf("append agent messages: %w", err)
		}
		appended = true
		return nil
	})
	return appended, err
}

const appendEvent = `WITH next AS (
    UPDATE agent_turns SET event_seq = event_seq + 1 WHERE id = @turn RETURNING event_seq),
added AS (
    INSERT INTO agent_turn_events (turn_id, seq, event_type, body, occurred_at)
    SELECT @turn, event_seq, @type, @body, @at FROM next RETURNING seq)
SELECT seq, pg_notify(@channel, @ring) FROM added`

func eventArgs(turn uuid.UUID, typ string, body json.RawMessage, at time.Time) pgx.StrictNamedArgs {
	if body == nil {
		body = json.RawMessage(`{}`)
	}
	return pgx.StrictNamedArgs{
		"turn": turn, "type": typ, "body": body, "at": at, "channel": turnChannel, "ring": turn.String(),
	}
}

func (s pgConversations) AppendEvent(ctx context.Context, turn uuid.UUID, typ string, body json.RawMessage, at time.Time) (Event, error) {
	if body == nil {
		body = json.RawMessage(`{}`)
	}
	e := Event{TurnID: turn, Type: typ, Body: body, At: at}
	a := eventArgs(turn, typ, body, at)
	err := s.db.QueryRow(ctx, appendEvent, a).Scan(&e.Seq, nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("insert agent turn event: %w", err)
	}
	return e, nil
}

func (s pgConversations) Events(ctx context.Context, turn uuid.UUID, after int64, limit int) ([]Event, error) {
	rows, err := s.db.Query(ctx, `SELECT turn_id, seq, event_type, body, occurred_at FROM agent_turn_events
WHERE turn_id = @turn AND seq > @after ORDER BY seq LIMIT @limit`, pgx.StrictNamedArgs{"turn": turn, "after": after, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select agent turn events: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		scanErr := r.Scan(&e.TurnID, &e.Seq, &e.Type, &e.Body, &e.At)
		return e, scanned(scanErr)
	})
	if err != nil {
		return nil, fmt.Errorf("read agent turn events: %w", err)
	}
	return out, nil
}

func (s pgConversations) FinishRun(ctx context.Context, e RunEnd, typ string, body json.RawMessage) (bool, error) {
	finished := false
	err := inTx(ctx, s.db, "finish agent run", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE agent_turns SET status = @status, stop_reason = NULLIF(@stop, ''),
    error_text = NULLIF(@error, ''), token_hash = NULL, finished_at = @at, input_tokens = input_tokens + @input,
    output_tokens = output_tokens + @output, cached_tokens = cached_tokens + @cached
WHERE id = @id AND status = 'running' AND run_started_at = @run`, pgx.StrictNamedArgs{
			"id": e.TurnID, "run": e.RunStartedAt, "status": e.Status, "stop": e.Stop, "error": e.Error, "at": e.At,
			"input": e.Usage.InputTokens, "output": e.Usage.OutputTokens, "cached": e.Usage.CachedTokens,
		})
		if err != nil {
			return fmt.Errorf("finish agent run: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if err = tx.QueryRow(ctx, appendEvent, eventArgs(e.TurnID, typ, body, e.At)).Scan(nil, nil); err != nil {
			return fmt.Errorf("insert agent turn event: %w", err)
		}
		finished = true
		return nil
	})
	return finished, err
}

func (s pgConversations) StuckTurns(ctx context.Context, at time.Time, grace time.Duration, limit int) ([]Stuck, error) {
	rows, err := s.db.Query(ctx, `SELECT id, run_started_at FROM agent_turns
WHERE status = 'running' AND deadline_at < @before ORDER BY deadline_at LIMIT @limit`,
		pgx.StrictNamedArgs{"before": at.Add(-grace), "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select stuck agent turns: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Stuck, error) {
		var st Stuck
		scanErr := r.Scan(&st.TurnID, &st.RunStartedAt)
		return st, scanned(scanErr)
	})
	if err != nil {
		return nil, fmt.Errorf("read stuck agent turns: %w", err)
	}
	return out, nil
}

func (s pgConversations) ListenTurns(ctx context.Context, ring func(uuid.UUID), resync func()) {
	postgres.Listen(ctx, s.db, turnChannel, s.log, resync, func(payload string) {
		if id, err := uuid.Parse(payload); err == nil {
			ring(id)
		}
	})
}

func (s pgConversations) RecordCall(ctx context.Context, c Call) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO agent_turn_calls (turn_id, operation, method, path, status, took_ms, called_at)
VALUES (@turn, @operation, @method, @path, @status, @took, @at)`, pgx.StrictNamedArgs{
		"turn": c.TurnID, "operation": c.Operation, "method": c.Method, "path": c.Path, "status": c.Status,
		"took": c.TookMS, "at": c.At,
	}); err != nil {
		return fmt.Errorf("insert agent turn call: %w", err)
	}
	return nil
}

// blanked replaces a command's output once it is past keeping.
const blanked = "[removed: kept 90 days]"

func (s pgConversations) BlankOutputsBefore(ctx context.Context, before time.Time, limit int) (int, error) {
	a := pgx.StrictNamedArgs{"before": before, "limit": limit, "blank": blanked}
	msgs, err := s.db.Exec(ctx, `UPDATE agent_messages SET message = jsonb_set(message, '{content}', to_jsonb(@blank::text))
WHERE (conversation_id, seq) IN (SELECT conversation_id, seq FROM agent_messages
    WHERE created_at < @before AND message ->> 'role' = 'tool' AND message ->> 'content' <> '' AND message ->> 'content' <> @blank
    LIMIT @limit)`, a)
	if err != nil {
		return 0, fmt.Errorf("blank old agent command outputs: %w", err)
	}
	events, err := s.db.Exec(ctx, `UPDATE agent_turn_events SET body = jsonb_set(body, '{output}', to_jsonb(@blank::text))
WHERE (turn_id, seq) IN (SELECT turn_id, seq FROM agent_turn_events
    WHERE occurred_at < @before AND event_type = 'tool.finished' AND body ->> 'output' <> '' AND body ->> 'output' <> @blank
    LIMIT @limit)`, a)
	if err != nil {
		return 0, fmt.Errorf("blank old agent command events: %w", err)
	}
	return int(msgs.RowsAffected() + events.RowsAffected()), nil
}

func (s pgConversations) BlankAccountOutputs(ctx context.Context, account uuid.UUID) error {
	a := pgx.StrictNamedArgs{"account": account, "blank": blanked}
	if _, err := s.db.Exec(ctx, `UPDATE agent_messages SET message = jsonb_set(message, '{content}', to_jsonb(@blank::text))
WHERE conversation_id IN (SELECT id FROM agent_conversations WHERE subject_kind = 'account' AND subject_id = @account)
    AND message ->> 'role' = 'tool' AND message ->> 'content' <> @blank`, a); err != nil {
		return fmt.Errorf("blank the account's agent command outputs: %w", err)
	}
	if _, err := s.db.Exec(ctx, `UPDATE agent_turn_events SET body = jsonb_set(body, '{output}', to_jsonb(@blank::text))
WHERE turn_id IN (SELECT t.id FROM agent_turns AS t JOIN agent_conversations AS c ON c.id = t.conversation_id
        WHERE c.subject_kind = 'account' AND c.subject_id = @account)
    AND event_type = 'tool.finished' AND body ->> 'output' <> @blank`, a); err != nil {
		return fmt.Errorf("blank the account's agent command events: %w", err)
	}
	return nil
}

// DeleteIdleConversations skips a conversation a write holds.
func (s pgConversations) DeleteIdleConversations(ctx context.Context, before time.Time, limit int) (int, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM agent_conversations WHERE id IN (
    SELECT id FROM agent_conversations WHERE last_activity_at < @before LIMIT @limit FOR UPDATE SKIP LOCKED)`,
		pgx.StrictNamedArgs{"before": before, "limit": limit})
	if err != nil {
		return 0, fmt.Errorf("delete idle agent conversations: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (s pgConversations) DeleteCallsBefore(ctx context.Context, before time.Time, limit int) (int, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM agent_turn_calls WHERE id IN (
    SELECT id FROM agent_turn_calls WHERE called_at < @before LIMIT @limit)`, pgx.StrictNamedArgs{"before": before, "limit": limit})
	if err != nil {
		return 0, fmt.Errorf("delete old agent turn calls: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (s pgConversations) RecordLimit(ctx context.Context, member uuid.UUID, at time.Time) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO agent_rate_limits (member_id, limited_at) VALUES (@member, @at)`,
		pgx.StrictNamedArgs{"member": member, "at": at}); err != nil {
		return fmt.Errorf("insert agent rate limit: %w", err)
	}
	return nil
}

func (s pgConversations) DeleteLimitsBefore(ctx context.Context, before time.Time, limit int) (int, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM agent_rate_limits WHERE id IN (
    SELECT id FROM agent_rate_limits WHERE limited_at < @before LIMIT @limit)`, pgx.StrictNamedArgs{"before": before, "limit": limit})
	if err != nil {
		return 0, fmt.Errorf("delete old agent rate limits: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (s pgConversations) Activity(ctx context.Context, from time.Time) (Activity, error) {
	args := pgx.StrictNamedArgs{"from": from}
	var out Activity
	rows, err := s.db.Query(ctx, `SELECT t.agent_id, c.operation, count(*)::int,
    (count(*) FILTER (WHERE c.status = 202))::int, (count(*) FILTER (WHERE c.status >= 400))::int,
    round(avg(c.took_ms))::int, max(c.called_at)
FROM agent_turn_calls AS c JOIN agent_turns AS t ON t.id = c.turn_id
WHERE c.called_at >= @from GROUP BY 1, 2 ORDER BY 3 DESC, 2`, args)
	if err != nil {
		return Activity{}, fmt.Errorf("count agent calls: %w", err)
	}
	if out.Calls, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (CallCount, error) {
		var c CallCount
		return c, scanned(r.Scan(&c.AgentID, &c.Operation, &c.Calls, &c.Held, &c.Errors, &c.AvgMS, &c.LastAt))
	}); err != nil {
		return Activity{}, fmt.Errorf("read agent calls: %w", err)
	}
	rows, err = s.db.Query(ctx, `SELECT agent_id, status, coalesce(stop_reason, ''), count(*)::int
FROM agent_turns WHERE started_at >= @from GROUP BY 1, 2, 3 ORDER BY 4 DESC, 2, 3`, args)
	if err != nil {
		return Activity{}, fmt.Errorf("count agent turns: %w", err)
	}
	if out.Turns, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (TurnCount, error) {
		var t TurnCount
		return t, scanned(r.Scan(&t.AgentID, &t.Status, &t.Stop, &t.Turns))
	}); err != nil {
		return Activity{}, fmt.Errorf("read agent turns: %w", err)
	}
	rows, err = s.db.Query(ctx, `SELECT t.agent_id, a.status, count(*)::int
FROM agent_approvals AS a JOIN agent_turns AS t ON t.id = a.turn_id
WHERE a.created_at >= @from GROUP BY 1, 2 ORDER BY 3 DESC, 2`, args)
	if err != nil {
		return Activity{}, fmt.Errorf("count agent approvals: %w", err)
	}
	if out.Approvals, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ApprovalCount, error) {
		var a ApprovalCount
		return a, scanned(r.Scan(&a.AgentID, &a.Status, &a.Approvals))
	}); err != nil {
		return Activity{}, fmt.Errorf("read agent approvals: %w", err)
	}
	if err = s.db.QueryRow(ctx, `SELECT count(*)::int FROM agent_rate_limits WHERE limited_at >= @from`, args).
		Scan(&out.RateLimited); err != nil {
		return Activity{}, fmt.Errorf("count agent rate limits: %w", err)
	}
	return out, nil
}

func (s pgConversations) AgentDays(ctx context.Context, from time.Time) ([]DayCount, error) {
	rows, err := s.db.Query(ctx, `SELECT agent_id::text, to_char(timezone('UTC', started_at), 'YYYY-MM-DD'), count(*)::int
FROM agent_turns WHERE started_at >= @from GROUP BY 1, 2`, pgx.StrictNamedArgs{"from": from})
	if err != nil {
		return nil, fmt.Errorf("count agent turns by day: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (DayCount, error) {
		var d DayCount
		scanErr := r.Scan(&d.Key, &d.Date, &d.Count)
		return d, scanned(scanErr)
	})
	if err != nil {
		return nil, fmt.Errorf("read agent turns by day: %w", err)
	}
	return out, nil
}
