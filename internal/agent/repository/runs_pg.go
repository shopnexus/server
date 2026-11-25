package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/postgres"
)

// turnChannel is the channel a turn's new event rings on, its id the
// payload.
const turnChannel = "agent_turn"

// querier is a pool or a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type pgRuns struct {
	db  *pgxpool.Pool
	log *slog.Logger
}

// NewRuns keeps the console agent's runs on db; log is for ListenTurns.
func NewRuns(db *pgxpool.Pool, log *slog.Logger) Runs { return pgRuns{db: db, log: log} }

func isUnique(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == "23505"
}

const conversationColumns = `id, member_id, COALESCE(subject_kind, ''), subject_id, COALESCE(title, ''), status, created_at,
    updated_at, last_activity_at`

func scanConversation(r pgx.CollectableRow) (Conversation, error) {
	var c Conversation
	if err := r.Scan(&c.ID, &c.MemberID, &c.SubjectKind, &c.SubjectID, &c.Title, &c.Status, &c.CreatedAt, &c.UpdatedAt,
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

func (s pgRuns) OpenConversation(ctx context.Context, c Conversation) (Conversation, error) {
	var kind *string
	if c.SubjectKind != "" {
		kind = &c.SubjectKind
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO agent_conversations (id, member_id, subject_kind, subject_id, status, created_at,
    updated_at, last_activity_at)
VALUES (@id, @member, @kind, @subject, 'active', @at, @at, @at) ON CONFLICT DO NOTHING`, pgx.StrictNamedArgs{
		"id": c.ID, "member": c.MemberID, "kind": kind, "subject": c.SubjectID, "at": c.CreatedAt,
	}); err != nil {
		return Conversation{}, fmt.Errorf("insert agent conversation: %w", err)
	}
	if kind == nil {
		return s.Conversation(ctx, c.ID)
	}
	return oneConversation(s.db.Query(ctx, `SELECT `+conversationColumns+` FROM agent_conversations
WHERE subject_kind = @kind AND subject_id = @subject`, pgx.StrictNamedArgs{"kind": kind, "subject": c.SubjectID}))
}

func (s pgRuns) Conversations(ctx context.Context, member uuid.UUID, f ConversationFilter, limit int) ([]Conversation, error) {
	rows, err := s.db.Query(ctx, `SELECT `+conversationColumns+` FROM agent_conversations
WHERE status = 'active' AND CASE WHEN @kind = '' THEN subject_kind IS NULL AND member_id = @member
        AND (@mention_kind = '' OR id IN (SELECT conversation_id FROM agent_turns
            WHERE attachments @> jsonb_build_array(jsonb_build_object('kind', @mention_kind::text, 'id', @mention_id::uuid))))
    ELSE subject_kind = @kind AND (@subject::uuid IS NULL OR subject_id = @subject) END
ORDER BY last_activity_at DESC, id LIMIT @limit`, pgx.StrictNamedArgs{
		"member": member, "kind": f.SubjectKind, "subject": f.SubjectID, "mention_kind": f.MentionKind, "mention_id": f.MentionID,
		"limit": limit,
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

func (s pgRuns) Conversation(ctx context.Context, id uuid.UUID) (Conversation, error) {
	return oneConversation(s.db.Query(ctx, `SELECT `+conversationColumns+` FROM agent_conversations WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}))
}

func (s pgRuns) RenameConversation(ctx context.Context, id uuid.UUID, title string, at time.Time) (Conversation, error) {
	return oneConversation(s.db.Query(ctx, `UPDATE agent_conversations SET title = @title, updated_at = @at WHERE id = @id
RETURNING `+conversationColumns, pgx.StrictNamedArgs{"id": id, "title": title, "at": at}))
}

func (s pgRuns) DeleteConversation(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete agent conversation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var open bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agent_turns
    WHERE conversation_id = c.id AND status IN ('running', 'paused', 'awaiting_input'))
FROM agent_conversations AS c WHERE c.id = @id FOR UPDATE`, pgx.StrictNamedArgs{"id": id}).Scan(&open)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock agent conversation: %w", err)
	}
	if open {
		return ErrBusy
	}
	if _, err = tx.Exec(ctx, `DELETE FROM agent_conversations WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete agent conversation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete agent conversation: %w", err)
	}
	return nil
}

func (s pgRuns) OpenTurn(ctx context.Context, t Turn, member uuid.UUID) error {
	b := &pgx.Batch{}
	b.Queue(`SELECT status FROM agent_conversations
WHERE id = @id AND (subject_kind IS NOT NULL OR member_id = @member) FOR UPDATE`,
		pgx.StrictNamedArgs{"id": t.ConversationID, "member": member}).QueryRow(func(row pgx.Row) error {
		var status string
		err := row.Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock agent conversation: %w", err)
		}
		if status != "active" {
			return ErrClosed
		}
		return nil
	})
	b.Queue(`INSERT INTO agent_turns (id, conversation_id, member_id, ordinal, status, question, agent_id,
    agent_name, model_alias, started_at, run_started_at, deadline_ms, attachments)
SELECT @id, @conversation, @member, COALESCE(max(ordinal), -1) + 1, 'running', @question, @agent_id, @agent_name, @alias,
    @at, @at, @deadline, COALESCE(@attachments::jsonb, '[]')
FROM agent_turns WHERE conversation_id = @conversation`, pgx.StrictNamedArgs{
		"id": t.ID, "conversation": t.ConversationID, "member": t.MemberID, "question": t.Question, "agent_id": t.AgentID,
		"agent_name": t.AgentName, "alias": t.ModelAlias, "at": t.StartedAt, "deadline": t.DeadlineMS, "attachments": t.Attachments,
	}).Fn = func(br pgx.BatchResults) error {
		_, err := br.Exec()
		if isUnique(err) {
			return ErrBusy
		}
		if err != nil {
			return fmt.Errorf("insert agent turn: %w", err)
		}
		return nil
	}
	b.Queue(`UPDATE agent_conversations SET last_activity_at = @at,
    title = COALESCE(title, left(@question, 80)) WHERE id = @id`,
		pgx.StrictNamedArgs{"id": t.ConversationID, "at": t.StartedAt, "question": t.Question}).Fn = postgres.ExecAs("touch agent conversation", nil)
	if err := sendInTx(ctx, s.db, b); err != nil {
		return fmt.Errorf("open agent turn: %w", err)
	}
	return nil
}

// sendInTx sends b in a transaction, so a callback that fails it undoes
// all of it.
func sendInTx(ctx context.Context, db *pgxpool.Pool, b *pgx.Batch) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { //nolint:wrapcheck // Each statement wraps its own error.
		return tx.SendBatch(ctx, b).Close()
	})
}

// errSkip ends a batch whose guarded write found nothing to do; its tx
// rolls back and the method reports that it did not write.
var errSkip = errors.New("agent: nothing to write")

func (s pgRuns) DeleteTurn(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM agent_turns WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete agent turn: %w", err)
	}
	return nil
}

const turnColumns = `t.id, t.conversation_id, t.member_id, t.ordinal, t.status, t.question, t.answer, t.blocks, t.sources,
    COALESCE(t.agent_id, ''), COALESCE(t.agent_name, ''), COALESCE(t.model_alias, ''), COALESCE(t.error_code, ''),
    t.input_tokens, t.output_tokens, t.cache_read_tokens, t.cache_write_tokens, t.cost_usd::float8, t.first_token_ms,
    COALESCE(t.provider_name, ''), t.started_at, t.run_started_at, t.deadline_ms, t.finished_at, t.latency_ms, t.event_seq,
    t.wrote, t.state, t.attachments,
    NOT EXISTS (SELECT 1 FROM agent_turns AS l WHERE l.conversation_id = t.conversation_id AND l.ordinal > t.ordinal),
    c.member_id, COALESCE(c.subject_kind, ''), c.subject_id`

const turnFrom = ` FROM agent_turns AS t JOIN agent_conversations AS c ON c.id = t.conversation_id`

func scanTurn(r pgx.CollectableRow) (Turn, error) {
	var t Turn
	if err := r.Scan(&t.ID, &t.ConversationID, &t.MemberID, &t.Ordinal, &t.Status, &t.Question, &t.Answer, &t.Blocks,
		&t.Sources, &t.AgentID, &t.AgentName, &t.ModelAlias, &t.ErrorCode, &t.Usage.InputTokens, &t.Usage.OutputTokens,
		&t.Usage.CacheReadTokens, &t.Usage.CacheWriteTokens, &t.Usage.CostUSD, &t.Usage.FirstTokenMS, &t.Usage.Provider,
		&t.StartedAt, &t.RunStartedAt, &t.DeadlineMS, &t.FinishedAt, &t.LatencyMS, &t.EventSeq, &t.Wrote, &t.State,
		&t.Attachments, &t.Latest, &t.Owner, &t.SubjectKind, &t.SubjectID); err != nil {
		return t, fmt.Errorf("scan agent turn: %w", err)
	}
	return t, nil
}

const selectTurn = `SELECT ` + turnColumns + turnFrom + ` WHERE t.id = @id`

// collectTurn reads the one turn of rows, or ErrNotFound.
func collectTurn(rows pgx.Rows) (Turn, error) {
	t, err := pgx.CollectExactlyOneRow(rows, scanTurn)
	if errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, ErrNotFound
	}
	if err != nil {
		return Turn{}, fmt.Errorf("read agent turn: %w", err)
	}
	return t, nil
}

func (s pgRuns) Turn(ctx context.Context, id uuid.UUID) (Turn, error) {
	rows, err := s.db.Query(ctx, selectTurn, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return Turn{}, fmt.Errorf("select agent turn: %w", err)
	}
	return collectTurn(rows)
}

func (s pgRuns) Turns(ctx context.Context, conversation uuid.UUID) ([]Turn, error) {
	rows, err := s.db.Query(ctx, `SELECT `+turnColumns+turnFrom+` WHERE t.conversation_id = @id ORDER BY t.ordinal`,
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

const inputColumns = `seq, id, turn_id, kind, COALESCE(input_text, ''), answers, read_at, created_at`

func scanInput(r pgx.CollectableRow) (Input, error) {
	var in Input
	if err := r.Scan(&in.Seq, &in.ID, &in.TurnID, &in.Kind, &in.Text, &in.Answers, &in.ReadAt, &in.CreatedAt); err != nil {
		return in, fmt.Errorf("scan agent turn input: %w", err)
	}
	return in, nil
}

const selectInputs = `SELECT ` + inputColumns + ` FROM agent_turn_inputs WHERE `

func collectInputs(rows pgx.Rows) ([]Input, error) {
	out, err := pgx.CollectRows(rows, scanInput)
	if err != nil {
		return nil, fmt.Errorf("read agent turn inputs: %w", err)
	}
	return out, nil
}

func inputs(ctx context.Context, db querier, query string, args pgx.StrictNamedArgs) ([]Input, error) {
	rows, err := db.Query(ctx, selectInputs+query, args)
	if err != nil {
		return nil, fmt.Errorf("select agent turn inputs: %w", err)
	}
	return collectInputs(rows)
}

func (s pgRuns) Steers(ctx context.Context, turns []uuid.UUID) (map[uuid.UUID][]Input, error) {
	list, err := inputs(ctx, s.db, `turn_id = ANY(@turns) AND kind = 'steer' ORDER BY seq`, pgx.StrictNamedArgs{"turns": turns})
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]Input{}
	for i := range list {
		out[list[i].TurnID] = append(out[list[i].TurnID], list[i])
	}
	return out, nil
}

func (s pgRuns) History(ctx context.Context, conversation uuid.UUID, limit int) ([]History, error) {
	rows, err := s.db.Query(ctx, `SELECT id, status, question, COALESCE(answer, '') FROM agent_turns
WHERE conversation_id = @id AND status NOT IN ('running', 'paused', 'awaiting_input')
ORDER BY ordinal DESC LIMIT @limit`, pgx.StrictNamedArgs{"id": conversation, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select agent history: %w", err)
	}
	var ids []uuid.UUID
	var out []History
	for rows.Next() {
		var id uuid.UUID
		var h History
		if err = rows.Scan(&id, &h.Status, &h.Question, &h.Answer); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read agent history: %w", err)
		}
		ids, out = append(ids, id), append(out, h)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read agent history: %w", err)
	}
	slices.Reverse(ids)
	slices.Reverse(out)
	at := map[uuid.UUID]int{}
	for i, id := range ids {
		at[id] = i
	}
	read, err := inputs(ctx, s.db, `turn_id = ANY(@turns) AND kind IN ('steer', 'answers') AND read_at IS NOT NULL`,
		pgx.StrictNamedArgs{"turns": ids})
	if err != nil {
		return nil, err
	}
	for i := range read {
		in := &read[i]
		h := &out[at[in.TurnID]]
		h.Entries = append(h.Entries, HistoryEntry{Kind: "input", Text: in.Text, At: *in.ReadAt})
	}
	calls, err := s.db.Query(ctx, `SELECT turn_id, tool_use_id, tool_name, tool_input, status, result_content, tool_output,
    created_at
FROM agent_tool_calls WHERE turn_id = ANY(@turns) AND status IN ('success', 'failed')`, pgx.StrictNamedArgs{"turns": ids})
	if err != nil {
		return nil, fmt.Errorf("select agent history calls: %w", err)
	}
	defer calls.Close()
	for calls.Next() {
		var turn uuid.UUID
		var e HistoryEntry
		var output *string
		if err = calls.Scan(&turn, &e.ToolUseID, &e.Tool, &e.Input, &e.Status, &e.Content, &output, &e.At); err != nil {
			return nil, fmt.Errorf("read agent history call: %w", err)
		}
		e.Kind = "tool"
		if output != nil {
			e.Text = *output
		}
		h := &out[at[turn]]
		h.Entries = append(h.Entries, e)
	}
	if err = calls.Err(); err != nil {
		return nil, fmt.Errorf("read agent history calls: %w", err)
	}
	for i := range out {
		slices.SortStableFunc(out[i].Entries, func(a, b HistoryEntry) int { return a.At.Compare(b.At) })
	}
	return out, nil
}

// documentsOf selects the documents of the conversation expression c.
func documentsOf(c string) string {
	return `SELECT id, latest_version FROM agent_documents WHERE conversation_id = ` + c + ` ORDER BY created_at, id`
}

func collectDocumentRefs(rows pgx.Rows) ([]DocumentRef, error) {
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (DocumentRef, error) {
		var d DocumentRef
		if scanErr := r.Scan(&d.ID, &d.LatestVersion); scanErr != nil {
			return d, fmt.Errorf("scan agent document: %w", scanErr)
		}
		return d, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read agent documents: %w", err)
	}
	return out, nil
}

func (s pgRuns) Documents(ctx context.Context, conversation uuid.UUID) ([]DocumentRef, error) {
	rows, err := s.db.Query(ctx, documentsOf(`@id`), pgx.StrictNamedArgs{"id": conversation})
	if err != nil {
		return nil, fmt.Errorf("select agent documents: %w", err)
	}
	return collectDocumentRefs(rows)
}

// selectMentions reads the attachments of @turn's conversation, @turn's
// first, up to @limit.
const selectMentions = `SELECT kind, id FROM (
    SELECT DISTINCT ON (e.a->>'kind', e.a->>'id') e.a->>'kind' AS kind, (e.a->>'id')::uuid AS id, t.id = @turn AS current,
        t.ordinal, e.n
    FROM agent_turns AS t CROSS JOIN LATERAL jsonb_array_elements(t.attachments) WITH ORDINALITY AS e(a, n)
    WHERE t.conversation_id = (SELECT conversation_id FROM agent_turns WHERE id = @turn)
    ORDER BY e.a->>'kind', e.a->>'id', t.id = @turn DESC, t.ordinal DESC, e.n
) AS m ORDER BY current DESC, ordinal DESC, n LIMIT @limit`

func collectMentions(rows pgx.Rows) ([]domain.Attachment, error) {
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Attachment, error) {
		var a domain.Attachment
		if scanErr := r.Scan(&a.Kind, &a.ID); scanErr != nil {
			return a, fmt.Errorf("scan agent mention: %w", scanErr)
		}
		return a, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read agent mentions: %w", err)
	}
	return out, nil
}

func (s pgRuns) Mentions(ctx context.Context, turn uuid.UUID, limit int) ([]domain.Attachment, error) {
	rows, err := s.db.Query(ctx, selectMentions, pgx.StrictNamedArgs{"turn": turn, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select agent mentions: %w", err)
	}
	return collectMentions(rows)
}

func (s pgRuns) ResumeTurn(ctx context.Context, r Resume) (Resumed, error) {
	// The state check only aborts, so the writes and the reads of the
	// resumed turn ride in its batch.
	b := &pgx.Batch{}
	b.Queue(`SELECT t.status = @from AND (t.status IN ('paused', 'awaiting_input') OR (
    NOT EXISTS (SELECT 1 FROM agent_turns AS l WHERE l.conversation_id = t.conversation_id AND l.ordinal > t.ordinal)
    AND NOT EXISTS (SELECT 1 FROM agent_turns AS o WHERE o.conversation_id = t.conversation_id
        AND o.status IN ('running', 'paused', 'awaiting_input'))))
FROM agent_turns AS t WHERE t.id = @id FOR UPDATE`, pgx.StrictNamedArgs{"id": r.TurnID, "from": r.From}).QueryRow(func(row pgx.Row) error {
		var ok bool
		err := row.Scan(&ok)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock agent turn: %w", err)
		}
		if !ok {
			return ErrState
		}
		return nil
	})
	answers := false
	if in := r.Input; in != nil {
		answers = in.Kind == "answers"
		var text *string
		if in.Text != "" {
			text = &in.Text
		}
		b.Queue(`INSERT INTO agent_turn_inputs (id, turn_id, kind, input_text, answers, read_at, created_at)
VALUES (@id, @turn, @kind, @text, @answers, @read_at, @at)`, pgx.StrictNamedArgs{
			"id": in.ID, "turn": in.TurnID, "kind": in.Kind, "text": text, "answers": in.Answers, "read_at": in.ReadAt,
			"at": in.CreatedAt,
		}).Fn = postgres.ExecAs("insert agent turn input", nil)
	}
	b.Queue(`UPDATE agent_turns SET status = 'running', run_started_at = @at, finished_at = NULL, error_code = NULL,
    answer = CASE WHEN @clear THEN NULL ELSE answer END, blocks = CASE WHEN @clear THEN NULL ELSE blocks END,
    state = CASE WHEN @answers THEN state - 'pending_ask' ELSE state END
WHERE id = @id`, pgx.StrictNamedArgs{"id": r.TurnID, "at": r.At, "clear": r.ClearAnswer, "answers": answers}).
		Fn = func(br pgx.BatchResults) error {
		_, err := br.Exec()
		if isUnique(err) {
			return ErrState
		}
		if err != nil {
			return fmt.Errorf("resume agent turn: %w", err)
		}
		return nil
	}
	var out Resumed
	id := pgx.StrictNamedArgs{"id": r.TurnID}
	b.Queue(selectTurn, id).Query(func(rows pgx.Rows) (err error) {
		out.Turn, err = collectTurn(rows)
		return err
	})
	b.Queue(selectInputs+`turn_id = @id AND kind = 'steer' AND read_at IS NULL ORDER BY seq`, id).
		Query(func(rows pgx.Rows) (err error) {
			out.Steers, err = collectInputs(rows)
			return err
		})
	b.Queue(`SELECT COALESCE(max(seq), 0) FROM agent_turn_inputs WHERE turn_id = @id`, id).QueryRow(func(row pgx.Row) error {
		if err := row.Scan(&out.ControlAfter); err != nil {
			return fmt.Errorf("read agent control seq: %w", err)
		}
		return nil
	})
	b.Queue(documentsOf(`(SELECT conversation_id FROM agent_turns WHERE id = @id)`), id).Query(func(rows pgx.Rows) (err error) {
		out.Documents, err = collectDocumentRefs(rows)
		return err
	})
	b.Queue(selectMentions, pgx.StrictNamedArgs{"turn": r.TurnID, "limit": domain.MaxMentions}).
		Query(func(rows pgx.Rows) (err error) {
			out.Mentions, err = collectMentions(rows)
			return err
		})
	if err := sendInTx(ctx, s.db, b); err != nil {
		return Resumed{}, fmt.Errorf("resume agent turn: %w", err)
	}
	return out, nil
}

func (s pgRuns) RestoreTurn(ctx context.Context, before Turn, input *uuid.UUID) error {
	var code *string
	if before.ErrorCode != "" {
		code = &before.ErrorCode
	}
	b := &pgx.Batch{}
	b.Queue(`UPDATE agent_turns SET status = @status, run_started_at = @run_started_at,
    finished_at = @finished_at, error_code = @code, answer = @answer, blocks = @blocks, state = @state
WHERE id = @id AND status = 'running'`, pgx.StrictNamedArgs{
		"id": before.ID, "status": before.Status, "run_started_at": before.RunStartedAt, "finished_at": before.FinishedAt,
		"code": code, "answer": before.Answer, "blocks": before.Blocks, "state": before.State,
	}).Fn = postgres.ExecAs("restore agent turn", nil)
	if input != nil {
		b.Queue(`DELETE FROM agent_turn_inputs WHERE id = @id`, pgx.StrictNamedArgs{"id": *input}).Fn = postgres.ExecAs("delete agent turn input", nil)
	}
	if err := s.db.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("restore agent turn: %w", err)
	}
	return nil
}

// appendEvent writes an event with the turn's next seq and rings it, only
// while the turn runs when @running is set; no row when it did not write.
const appendEvent = `WITH next AS (
    UPDATE agent_turns SET event_seq = event_seq + 1, wrote = wrote OR @wrote
    WHERE id = @turn AND (NOT @running OR status = 'running') RETURNING event_seq),
added AS (
    INSERT INTO agent_turn_events (turn_id, seq, event_type, body, occurred_at)
    SELECT @turn, event_seq, @type, @body, @at FROM next RETURNING seq)
SELECT seq, pg_notify(@channel, @ring) FROM added`

// eventArgs are appendEvent's arguments; a nil body is an empty object.
func eventArgs(turn uuid.UUID, typ string, body json.RawMessage, at time.Time, running, wrote bool) pgx.StrictNamedArgs {
	if body == nil {
		body = json.RawMessage(`{}`)
	}
	return pgx.StrictNamedArgs{
		"turn": turn, "type": typ, "body": body, "at": at, "running": running, "wrote": wrote,
		"channel": turnChannel, "ring": turn.String(),
	}
}

// touch moves a turn's conversation's last activity to at. A batch queues
// it before it writes the turn: OpenTurn locks the conversation first, and
// the other order deadlocks with it.
func touch(b *pgx.Batch, turn uuid.UUID, at time.Time) {
	b.Queue(`UPDATE agent_conversations SET last_activity_at = @at
WHERE id = (SELECT conversation_id FROM agent_turns WHERE id = @turn)`, pgx.StrictNamedArgs{"turn": turn, "at": at}).
		Fn = postgres.ExecAs("touch agent conversation", nil)
}

func (s pgRuns) CancelTurn(ctx context.Context, id uuid.UUID, from string, sources, body json.RawMessage, at time.Time) error {
	b := &pgx.Batch{}
	touch(b, id, at)
	b.Queue(`UPDATE agent_turns SET status = 'cancelled', finished_at = @at, answer = '', sources = @sources,
    state = state - 'pending_ask'
WHERE id = @id AND status = @from`, pgx.StrictNamedArgs{"id": id, "from": from, "sources": sources, "at": at}).
		Fn = postgres.ExecAs("cancel agent turn", ErrState)
	b.Queue(appendEvent, eventArgs(id, "turn.finished", body, at, false, false)).Fn = postgres.ExecAs("insert agent turn event", nil)
	if err := sendInTx(ctx, s.db, b); err != nil {
		return fmt.Errorf("cancel agent turn: %w", err)
	}
	return nil
}

func (s pgRuns) CloseStuck(ctx context.Context, id uuid.UUID, segment time.Time, code, answer string, body json.RawMessage,
	at time.Time,
) (bool, error) {
	b := &pgx.Batch{}
	touch(b, id, at)
	b.Queue(`UPDATE agent_turns SET status = 'failed', error_code = @code, answer = @answer, finished_at = @at,
    latency_ms = COALESCE(latency_ms, 0) + GREATEST(0, (extract(EPOCH FROM @at::timestamptz - run_started_at) * 1000)::bigint)
WHERE id = @id AND status = 'running' AND run_started_at = @segment`,
		pgx.StrictNamedArgs{"id": id, "segment": segment, "code": code, "answer": answer, "at": at}).Fn = postgres.ExecAs("close agent turn", errSkip)
	b.Queue(appendEvent, eventArgs(id, "turn.finished", body, at, false, false)).Fn = postgres.ExecAs("insert agent turn event", nil)
	err := sendInTx(ctx, s.db, b)
	if errors.Is(err, errSkip) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("close agent turn: %w", err)
	}
	return true, nil
}

func (s pgRuns) StuckTurns(ctx context.Context, at time.Time, grace time.Duration, limit int) ([]Stuck, error) {
	rows, err := s.db.Query(ctx, `SELECT id, run_started_at FROM agent_turns
WHERE status = 'running' AND run_started_at + make_interval(secs => (deadline_ms + @grace) / 1000.0) < @at
ORDER BY run_started_at LIMIT @limit`, pgx.StrictNamedArgs{"at": at, "grace": grace.Milliseconds(), "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select stuck agent turns: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Stuck, error) {
		var st Stuck
		if scanErr := r.Scan(&st.ID, &st.RunStartedAt); scanErr != nil {
			return st, fmt.Errorf("scan stuck agent turn: %w", scanErr)
		}
		return st, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read stuck agent turns: %w", err)
	}
	return out, nil
}

func (s pgRuns) AddInput(ctx context.Context, in Input) error {
	var text *string
	if in.Text != "" {
		text = &in.Text
	}
	tag, err := s.db.Exec(ctx, `INSERT INTO agent_turn_inputs (id, turn_id, kind, input_text, answers, read_at, created_at)
SELECT @id, @turn, @kind, @text, @answers, @read_at, @at WHERE EXISTS (
    SELECT 1 FROM agent_turns WHERE id = @turn AND status = 'running')`, pgx.StrictNamedArgs{
		"id": in.ID, "turn": in.TurnID, "kind": in.Kind, "text": text, "answers": in.Answers, "read_at": in.ReadAt,
		"at": in.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert agent turn input: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrState
	}
	return nil
}

func (s pgRuns) Control(ctx context.Context, turn uuid.UUID, after int64, limit int) ([]Input, bool, error) {
	var status string
	err := s.db.QueryRow(ctx, `SELECT status FROM agent_turns WHERE id = @id`, pgx.StrictNamedArgs{"id": turn}).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return []Input{}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("select agent turn status: %w", err)
	}
	list, err := inputs(ctx, s.db, `turn_id = @id AND seq > @after AND kind IN ('pause', 'stop', 'steer') ORDER BY seq LIMIT @limit`,
		pgx.StrictNamedArgs{"id": turn, "after": after, "limit": limit})
	if err != nil {
		return nil, false, err
	}
	return list, status == "running", nil
}

func (s pgRuns) AppendEvent(ctx context.Context, turn uuid.UUID, typ string, body json.RawMessage, at time.Time) (Event, error) {
	if body == nil {
		body = json.RawMessage(`{}`)
	}
	e := Event{TurnID: turn, Type: typ, Body: body, At: at}
	err := s.db.QueryRow(ctx, appendEvent, eventArgs(turn, typ, body, at, false, false)).Scan(&e.Seq, nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("insert agent turn event: %w", err)
	}
	return e, nil
}

func (s pgRuns) AppendRunEvent(ctx context.Context, turn uuid.UUID, typ string, body json.RawMessage, wrote bool,
	steer *uuid.UUID, at time.Time,
) (bool, error) {
	a := eventArgs(turn, typ, body, at, true, wrote)
	if steer == nil {
		err := s.db.QueryRow(ctx, appendEvent, a).Scan(nil, nil)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("insert agent turn event: %w", err)
		}
		return true, nil
	}
	// The steer counts as read only with the event that answers it.
	b := &pgx.Batch{}
	b.Queue(appendEvent, a).QueryRow(func(row pgx.Row) error {
		err := row.Scan(nil, nil)
		if errors.Is(err, pgx.ErrNoRows) {
			return errSkip
		}
		if err != nil {
			return fmt.Errorf("insert agent turn event: %w", err)
		}
		return nil
	})
	b.Queue(`UPDATE agent_turn_inputs SET read_at = @at
WHERE id = @id AND turn_id = @turn AND read_at IS NULL`, pgx.StrictNamedArgs{"id": *steer, "turn": turn, "at": at}).
		Fn = postgres.ExecAs("mark agent steer read", nil)
	err := sendInTx(ctx, s.db, b)
	if errors.Is(err, errSkip) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("append agent run event: %w", err)
	}
	return true, nil
}

func (s pgRuns) Events(ctx context.Context, turn uuid.UUID, after int64, limit int) ([]Event, error) {
	rows, err := s.db.Query(ctx, `SELECT turn_id, seq, event_type, body, occurred_at FROM agent_turn_events
WHERE turn_id = @turn AND seq > @after ORDER BY seq LIMIT @limit`, pgx.StrictNamedArgs{"turn": turn, "after": after, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select agent turn events: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		if scanErr := r.Scan(&e.TurnID, &e.Seq, &e.Type, &e.Body, &e.At); scanErr != nil {
			return e, fmt.Errorf("scan agent turn event: %w", scanErr)
		}
		return e, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read agent turn events: %w", err)
	}
	return out, nil
}

func (s pgRuns) Checkpoint(ctx context.Context, turn uuid.UUID, state json.RawMessage, wrote bool) error {
	if _, err := s.db.Exec(ctx, `UPDATE agent_turns SET state = @state, wrote = wrote OR @wrote
WHERE id = @id AND status = 'running'`, pgx.StrictNamedArgs{"id": turn, "state": state, "wrote": wrote}); err != nil {
		return fmt.Errorf("checkpoint agent turn: %w", err)
	}
	return nil
}

func (s pgRuns) SaveToolCall(ctx context.Context, w ToolWrite) error {
	if w.Input == nil {
		w.Input = json.RawMessage(`{}`)
	}
	// The call is kept only while its turn runs, which the turn's update
	// returns.
	if _, err := s.db.Exec(ctx, `WITH turn AS (
    UPDATE agent_turns SET wrote = TRUE WHERE id = @turn AND status = 'running' RETURNING id)
INSERT INTO agent_tool_calls AS c (id, turn_id, tool_use_id, tool_name, tool_label, tool_source,
    tool_input, tool_output, result_content, output_spilled, status, latency_ms, attempts, attempt_errors, tool_timing,
    created_at, finished_at)
SELECT @id, turn.id, @use, @tool, @label, @source, @input, @output, @content, COALESCE(@spilled, FALSE), @status, @latency,
    COALESCE(@attempts, 0), @attempt_errors, @timing, @at, @finished_at
FROM turn
ON CONFLICT (turn_id, tool_use_id) DO UPDATE SET tool_name = EXCLUDED.tool_name,
    tool_label = COALESCE(@label, c.tool_label), tool_source = COALESCE(@source, c.tool_source),
    tool_input = EXCLUDED.tool_input, tool_output = COALESCE(@output, c.tool_output),
    result_content = COALESCE(@content, c.result_content), output_spilled = COALESCE(@spilled, c.output_spilled),
    status = EXCLUDED.status, latency_ms = COALESCE(@latency, c.latency_ms), attempts = COALESCE(@attempts, c.attempts),
    attempt_errors = COALESCE(@attempt_errors, c.attempt_errors), tool_timing = COALESCE(@timing, c.tool_timing),
    finished_at = COALESCE(@finished_at, c.finished_at)
WHERE c.status IN ('pending', 'running') OR EXCLUDED.status NOT IN ('pending', 'running')`, pgx.StrictNamedArgs{
		"id": w.ID, "turn": w.TurnID, "use": w.ToolUseID, "tool": w.Tool, "label": w.Label, "source": w.Source,
		"input": w.Input, "output": w.Output, "content": w.Content, "spilled": w.OutputSpilled, "status": w.Status,
		"latency": w.LatencyMS, "attempts": w.Attempts, "attempt_errors": w.AttemptErrors, "timing": w.Timing, "at": w.At,
		"finished_at": w.FinishedAt,
	}); err != nil {
		return fmt.Errorf("save agent tool call: %w", err)
	}
	return nil
}

func (s pgRuns) WriteDocument(ctx context.Context, w DocumentWrite) (bool, error) {
	b := &pgx.Batch{}
	b.Queue(`SELECT EXISTS (SELECT 1 FROM agent_turns WHERE id = @turn AND status = 'running')`,
		pgx.StrictNamedArgs{"turn": w.TurnID}).QueryRow(func(row pgx.Row) error {
		var running bool
		if err := row.Scan(&running); err != nil {
			return fmt.Errorf("read agent turn status: %w", err)
		}
		if !running {
			return errSkip
		}
		return nil
	})
	q := `UPDATE agent_documents SET latest_version = @version, title = @title, turn_id = @turn, updated_at = @at
WHERE id = @id AND conversation_id = @conversation AND latest_version = @version - 1`
	a := pgx.StrictNamedArgs{
		"id": w.ID, "conversation": w.ConversationID, "turn": w.TurnID, "version": w.Version, "title": w.Title, "at": w.At,
	}
	if w.Version == 1 {
		q = `INSERT INTO agent_documents (id, conversation_id, turn_id, title, latest_version, created_by, created_at, updated_at)
VALUES (@id, @conversation, @turn, @title, 1, @member, @at, @at) ON CONFLICT (id) DO NOTHING`
		delete(a, "version")
		a["member"] = w.MemberID
	}
	b.Queue(q, a).Fn = postgres.ExecAs("write agent document", errSkip)
	b.Queue(`INSERT INTO agent_document_versions (document_id, version_no, title, sections, turn_id,
    created_by, created_at)
VALUES (@id, @version, @title, @sections, @turn, @member, @at)`, pgx.StrictNamedArgs{
		"id": w.ID, "version": w.Version, "title": w.Title, "sections": w.Sections, "turn": w.TurnID, "member": w.MemberID, "at": w.At,
	}).Fn = postgres.ExecAs("insert agent document version", nil)
	err := sendInTx(ctx, s.db, b)
	if errors.Is(err, errSkip) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("write agent document: %w", err)
	}
	return true, nil
}

func (s pgRuns) FinishRun(ctx context.Context, e RunEnd) (bool, error) {
	final := e.Status != "paused" && e.Status != "awaiting_input"
	var code, provider *string
	if e.ErrorCode != "" {
		code = &e.ErrorCode
	}
	if e.Usage.Provider != "" {
		provider = &e.Usage.Provider
	}
	var finished *time.Time
	if final {
		finished = &e.At
	}
	b := &pgx.Batch{}
	touch(b, e.TurnID, e.At)
	b.Queue(`UPDATE agent_turns SET status = @status, error_code = @code,
    answer = CASE WHEN @final THEN @answer ELSE answer END, blocks = CASE WHEN @final THEN @blocks ELSE blocks END,
    sources = COALESCE(@sources, sources), input_tokens = @input, output_tokens = @output, cache_read_tokens = @cache_read,
    cache_write_tokens = @cache_write, cost_usd = @cost, first_token_ms = COALESCE(@first_token, first_token_ms),
    provider_name = COALESCE(@provider, provider_name), latency_ms = @latency, wrote = wrote OR @wrote,
    state = COALESCE(@state, state), finished_at = @finished_at
WHERE id = @id AND status = 'running'`, pgx.StrictNamedArgs{
		"id": e.TurnID, "status": e.Status, "code": code, "final": final, "answer": e.Answer, "blocks": e.Blocks,
		"sources": e.Sources, "input": e.Usage.InputTokens, "output": e.Usage.OutputTokens,
		"cache_read": e.Usage.CacheReadTokens, "cache_write": e.Usage.CacheWriteTokens, "cost": e.Usage.CostUSD,
		"first_token": e.Usage.FirstTokenMS, "provider": provider, "latency": e.LatencyMS, "wrote": e.Wrote,
		"state": e.State, "finished_at": finished,
	}).Fn = postgres.ExecAs("finish agent run", errSkip)
	if e.EventType != "" {
		b.Queue(appendEvent, eventArgs(e.TurnID, e.EventType, e.EventBody, e.At, false, false)).Fn = postgres.ExecAs("insert agent turn event", nil)
	}
	err := sendInTx(ctx, s.db, b)
	if errors.Is(err, errSkip) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("finish agent run: %w", err)
	}
	return true, nil
}

// visible is the condition that the conversation c may be seen by @member.
const visible = `(c.subject_kind IS NOT NULL OR c.member_id = @member)`

func (s pgRuns) ToolCall(ctx context.Context, id, member uuid.UUID) (ToolCall, error) {
	var tc ToolCall
	err := s.db.QueryRow(ctx, `SELECT tc.id, tc.turn_id, tc.tool_use_id, tc.tool_name, COALESCE(tc.tool_label, tc.tool_name),
    tc.tool_input, tc.tool_output, tc.output_spilled, tc.status, tc.latency_ms, tc.attempts, tc.attempt_errors, tc.tool_timing,
    tc.created_at, tc.finished_at
FROM agent_tool_calls AS tc JOIN agent_turns AS t ON t.id = tc.turn_id JOIN agent_conversations AS c ON c.id = t.conversation_id
WHERE tc.id = @id AND `+visible, pgx.StrictNamedArgs{"id": id, "member": member}).Scan(&tc.ID, &tc.TurnID, &tc.ToolUseID,
		&tc.Tool, &tc.Label, &tc.Input, &tc.Output, &tc.OutputSpilled, &tc.Status, &tc.LatencyMS, &tc.Attempts,
		&tc.AttemptErrors, &tc.Timing, &tc.CreatedAt, &tc.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ToolCall{}, ErrNotFound
	}
	if err != nil {
		return ToolCall{}, fmt.Errorf("read agent tool call: %w", err)
	}
	return tc, nil
}

func (s pgRuns) Document(ctx context.Context, id, member uuid.UUID) (Document, error) {
	var d Document
	err := s.db.QueryRow(ctx, `SELECT d.id, d.conversation_id, d.turn_id, d.title, d.latest_version, d.created_by, d.created_at,
    d.updated_at
FROM agent_documents AS d JOIN agent_conversations AS c ON c.id = d.conversation_id WHERE d.id = @id AND `+visible,
		pgx.StrictNamedArgs{"id": id, "member": member}).Scan(&d.ID, &d.ConversationID, &d.TurnID, &d.Title, &d.LatestVersion,
		&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("read agent document: %w", err)
	}
	rows, err := s.db.Query(ctx, `SELECT version_no, title, sections, turn_id, created_by, created_at FROM agent_document_versions
WHERE document_id = @id ORDER BY version_no DESC`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return Document{}, fmt.Errorf("select agent document versions: %w", err)
	}
	d.Versions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DocumentVersion, error) {
		var v DocumentVersion
		if scanErr := r.Scan(&v.Version, &v.Title, &v.Sections, &v.TurnID, &v.CreatedBy, &v.CreatedAt); scanErr != nil {
			return v, fmt.Errorf("scan agent document version: %w", scanErr)
		}
		return v, nil
	})
	if err != nil {
		return Document{}, fmt.Errorf("read agent document versions: %w", err)
	}
	return d, nil
}

const skillColumns = `id, created_by, skill_name, slug, description, prompt, skill_variables, created_at, updated_at`

func scanSkill(r pgx.CollectableRow) (Skill, error) {
	var sk Skill
	if err := r.Scan(&sk.ID, &sk.CreatedBy, &sk.Name, &sk.Slug, &sk.Description, &sk.Prompt, &sk.Variables, &sk.CreatedAt,
		&sk.UpdatedAt); err != nil {
		return sk, fmt.Errorf("scan agent skill: %w", err)
	}
	return sk, nil
}

func (s pgRuns) Skills(ctx context.Context) ([]Skill, error) {
	rows, err := s.db.Query(ctx, `SELECT `+skillColumns+` FROM agent_skills ORDER BY skill_name, id LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("select agent skills: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanSkill)
	if err != nil {
		return nil, fmt.Errorf("read agent skills: %w", err)
	}
	return out, nil
}

func (s pgRuns) Skill(ctx context.Context, id uuid.UUID) (Skill, error) {
	rows, err := s.db.Query(ctx, `SELECT `+skillColumns+` FROM agent_skills WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return Skill{}, fmt.Errorf("select agent skill: %w", err)
	}
	sk, err := pgx.CollectExactlyOneRow(rows, scanSkill)
	if errors.Is(err, pgx.ErrNoRows) {
		return Skill{}, ErrNotFound
	}
	if err != nil {
		return Skill{}, fmt.Errorf("read agent skill: %w", err)
	}
	return sk, nil
}

func (s pgRuns) SaveSkill(ctx context.Context, sk Skill) error {
	_, err := s.db.Exec(ctx, `INSERT INTO agent_skills (id, created_by, skill_name, slug, description, prompt, skill_variables,
    created_at, updated_at)
VALUES (@id, @by, @name, @slug, @description, @prompt, @variables, @created_at, @updated_at)
ON CONFLICT (id) DO UPDATE SET skill_name = EXCLUDED.skill_name, slug = EXCLUDED.slug, description = EXCLUDED.description,
    prompt = EXCLUDED.prompt, skill_variables = EXCLUDED.skill_variables, updated_at = EXCLUDED.updated_at`,
		pgx.StrictNamedArgs{
			"id": sk.ID, "by": sk.CreatedBy, "name": sk.Name, "slug": sk.Slug, "description": sk.Description, "prompt": sk.Prompt,
			"variables": sk.Variables, "created_at": sk.CreatedAt, "updated_at": sk.UpdatedAt,
		})
	if isUnique(err) {
		return ErrTaken
	}
	if err != nil {
		return fmt.Errorf("save agent skill: %w", err)
	}
	return nil
}

func (s pgRuns) DeleteSkill(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM agent_skills WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete agent skill: %w", err)
	}
	return nil
}

// day is a timestamptz column's UTC day as YYYY-MM-DD.
func day(col string) string { return `to_char(timezone('UTC', ` + col + `), 'YYYY-MM-DD')` }

func dayCounts(ctx context.Context, db querier, query string, from time.Time) ([]DayCount, error) {
	rows, err := db.Query(ctx, query, pgx.StrictNamedArgs{"from": from})
	if err != nil {
		return nil, fmt.Errorf("select agent day counts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (DayCount, error) {
		var d DayCount
		if scanErr := r.Scan(&d.Key, &d.Date, &d.Count); scanErr != nil {
			return d, fmt.Errorf("scan agent day count: %w", scanErr)
		}
		return d, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read agent day counts: %w", err)
	}
	return out, nil
}

func (s pgRuns) ToolTotals(ctx context.Context, from time.Time) ([]ToolTotal, error) {
	rows, err := s.db.Query(ctx, `SELECT tool_name, count(*)::int, (count(*) FILTER (WHERE status = 'failed'))::int,
    (percentile_cont(0.5) WITHIN GROUP (ORDER BY latency_ms))::float8
FROM agent_tool_calls WHERE created_at >= @from GROUP BY tool_name ORDER BY 2 DESC, 1`, pgx.StrictNamedArgs{"from": from})
	if err != nil {
		return nil, fmt.Errorf("select agent tool totals: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ToolTotal, error) {
		var t ToolTotal
		if scanErr := r.Scan(&t.Name, &t.Calls, &t.Failed, &t.P50MS); scanErr != nil {
			return t, fmt.Errorf("scan agent tool total: %w", scanErr)
		}
		return t, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read agent tool totals: %w", err)
	}
	return out, nil
}

func (s pgRuns) ToolDays(ctx context.Context, from time.Time) ([]DayCount, error) {
	return dayCounts(ctx, s.db, `SELECT tool_name, `+day("created_at")+`, count(*)::int FROM agent_tool_calls
WHERE created_at >= @from GROUP BY 1, 2`, from)
}

func (s pgRuns) RecentCalls(ctx context.Context, from time.Time, limit int) ([]RecentCall, error) {
	rows, err := s.db.Query(ctx, `SELECT c.id, c.turn_id, t.member_id, c.tool_name, COALESCE(c.tool_label, c.tool_name),
    c.latency_ms, c.status, COALESCE(c.finished_at, c.created_at)
FROM agent_tool_calls AS c JOIN agent_turns AS t ON t.id = c.turn_id
WHERE c.created_at >= @from ORDER BY c.created_at DESC, c.id LIMIT @limit`, pgx.StrictNamedArgs{"from": from, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select recent agent tool calls: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RecentCall, error) {
		var c RecentCall
		if scanErr := r.Scan(&c.CallID, &c.TurnID, &c.MemberID, &c.Tool, &c.Label, &c.DurationMS, &c.Status, &c.At); scanErr != nil {
			return c, fmt.Errorf("scan recent agent tool call: %w", scanErr)
		}
		return c, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read recent agent tool calls: %w", err)
	}
	return out, nil
}

func (s pgRuns) GuardrailRules(ctx context.Context, from time.Time) ([]RuleCount, error) {
	rows, err := s.db.Query(ctx, `SELECT COALESCE(body ->> 'rule', 'unknown'), count(*)::int FROM agent_turn_events
WHERE event_type = 'guardrail' AND occurred_at >= @from GROUP BY 1 ORDER BY 2 DESC, 1`, pgx.StrictNamedArgs{"from": from})
	if err != nil {
		return nil, fmt.Errorf("select agent guardrail rules: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RuleCount, error) {
		var c RuleCount
		if scanErr := r.Scan(&c.Rule, &c.Count); scanErr != nil {
			return c, fmt.Errorf("scan agent guardrail rule: %w", scanErr)
		}
		return c, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read agent guardrail rules: %w", err)
	}
	return out, nil
}

func (s pgRuns) RecentGuardrails(ctx context.Context, from time.Time, limit int) ([]RecentGuardrail, error) {
	rows, err := s.db.Query(ctx, `SELECT COALESCE(e.body ->> 'rule', 'unknown'), COALESCE(e.body ->> 'detail', ''), t.member_id,
    e.turn_id, e.occurred_at
FROM agent_turn_events AS e JOIN agent_turns AS t ON t.id = e.turn_id
WHERE e.event_type = 'guardrail' AND e.occurred_at >= @from ORDER BY e.occurred_at DESC, e.turn_id, e.seq DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"from": from, "limit": limit})
	if err != nil {
		return nil, fmt.Errorf("select recent agent guardrails: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RecentGuardrail, error) {
		var g RecentGuardrail
		if scanErr := r.Scan(&g.Rule, &g.Detail, &g.MemberID, &g.TurnID, &g.At); scanErr != nil {
			return g, fmt.Errorf("scan recent agent guardrail: %w", scanErr)
		}
		return g, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read recent agent guardrails: %w", err)
	}
	return out, nil
}

func (s pgRuns) AgentDays(ctx context.Context, from time.Time) ([]DayCount, error) {
	return dayCounts(ctx, s.db, `SELECT agent_id, `+day("started_at")+`, count(*)::int FROM agent_turns
WHERE started_at >= @from GROUP BY 1, 2`, from)
}

func (s pgRuns) MemberTurns(ctx context.Context, from time.Time) ([]MemberCount, error) {
	rows, err := s.db.Query(ctx, `SELECT member_id, count(*)::int FROM agent_turns WHERE started_at >= @from
GROUP BY 1 ORDER BY 2 DESC, 1`, pgx.StrictNamedArgs{"from": from})
	if err != nil {
		return nil, fmt.Errorf("select agent member turns: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MemberCount, error) {
		var m MemberCount
		if scanErr := r.Scan(&m.MemberID, &m.Count); scanErr != nil {
			return m, fmt.Errorf("scan agent member turns: %w", scanErr)
		}
		return m, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read agent member turns: %w", err)
	}
	return out, nil
}

func (s pgRuns) ListenTurns(ctx context.Context, ring func(uuid.UUID), resync func()) {
	postgres.Listen(ctx, s.db, turnChannel, s.log, resync, func(payload string) {
		if id, err := uuid.Parse(payload); err == nil {
			ring(id)
		}
	})
}
