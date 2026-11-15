package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/stream"
)

type pgChats struct{ db *pgxpool.Pool }

func NewChats(db *pgxpool.Pool) Chats { return pgChats{db} }

const messageColumns = `m.id, m.conversation_id, m.sender_id, m.body, m.photos, m.listing_id, m.reply_to, m.status,
       m.failure, m.created_at, m.deal_id, m.move_kind, m.move_by, m.move_amount`

// messageScan holds a message's columns while a row is scanned.
type messageScan struct {
	st         domain.MessageState
	photos     []byte
	failure    *string
	dealID     *uuid.UUID
	moveKind   *string
	moveBy     *string
	moveAmount *int64
}

func (m *messageScan) dest() []any {
	return []any{
		&m.st.ID, &m.st.ConversationID, &m.st.SenderID, &m.st.Body, &m.photos, &m.st.ListingID, &m.st.ReplyTo,
		&m.st.Status, &m.failure, &m.st.CreatedAt, &m.dealID, &m.moveKind, &m.moveBy, &m.moveAmount,
	}
}

// message is the scanned message.
func (m *messageScan) message() (domain.Message, error) {
	if err := json.Unmarshal(m.photos, &m.st.Photos); err != nil {
		return domain.Message{}, fmt.Errorf("decode photos: %w", err)
	}
	if m.failure != nil {
		m.st.Failure = *m.failure
	}
	if m.dealID != nil {
		m.st.Deal = &domain.DealMove{DealID: *m.dealID, Kind: *m.moveKind, By: *m.moveBy}
		if m.moveAmount != nil {
			m.st.Deal.Amount = *m.moveAmount
		}
	}
	return domain.RestoreMessage(m.st), nil
}

func scanMessage(row pgx.CollectableRow) (domain.Message, error) {
	var m messageScan
	if err := row.Scan(m.dest()...); err != nil {
		return domain.Message{}, fmt.Errorf("scan message: %w", err)
	}
	return m.message()
}

func (s pgChats) OpenPair(ctx context.Context, c domain.Conversation) (domain.Conversation, error) {
	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, kind, created_at)
VALUES (@id, @a, @b, 'pair', @at) ON CONFLICT (account_a, account_b) WHERE kind = 'pair' DO NOTHING`,
			pgx.StrictNamedArgs{"id": c.ID, "a": c.AccountA, "b": c.AccountB, "at": c.CreatedAt}); err != nil {
			return fmt.Errorf("insert conversation: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT id, account_a, account_b, kind, created_at FROM conversations
WHERE account_a = @a AND account_b = @b AND kind = 'pair'`, pgx.StrictNamedArgs{"a": c.AccountA, "b": c.AccountB}).
			Scan(&out.ID, &out.AccountA, &out.AccountB, &out.Kind, &out.CreatedAt); err != nil {
			return fmt.Errorf("select conversation: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_members (conversation_id, account_id)
VALUES (@id, @a), (@id, @b) ON CONFLICT DO NOTHING`, pgx.StrictNamedArgs{"id": out.ID, "a": out.AccountA, "b": out.AccountB}); err != nil {
			return fmt.Errorf("insert members: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("open conversation: %w", err)
	}
	return out, nil
}

// errKept ends a DropPair whose conversation something refers to.
var errKept = errors.New("chat: conversation in use")

func (s pgChats) DropPair(ctx context.Context, id uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM conversation_members WHERE conversation_id = @id`,
			pgx.StrictNamedArgs{"id": id}); err != nil {
			return fmt.Errorf("delete members: %w", err)
		}
		// The foreign keys of messages, deals, reports and tickets keep a
		// conversation in use, and the rollback restores its members.
		_, err := tx.Exec(ctx, `DELETE FROM conversations WHERE id = @id AND kind = 'pair'`, pgx.StrictNamedArgs{"id": id})
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23503" {
			return errKept
		}
		if err != nil {
			return fmt.Errorf("delete conversation: %w", err)
		}
		return nil
	})
	if err != nil && !errors.Is(err, errKept) {
		return fmt.Errorf("drop conversation: %w", err)
	}
	return nil
}

func (s pgChats) Get(ctx context.Context, id uuid.UUID) (domain.Conversation, error) {
	var c domain.Conversation
	err := s.db.QueryRow(ctx, `SELECT id, account_a, account_b, kind, created_at FROM conversations WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}).Scan(&c.ID, &c.AccountA, &c.AccountB, &c.Kind, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("select conversation: %w", err)
	}
	return c, nil
}

func (s pgChats) FindPair(ctx context.Context, one, other uuid.UUID) (domain.Conversation, error) {
	var c domain.Conversation
	err := s.db.QueryRow(ctx, `SELECT id, account_a, account_b, kind, created_at FROM conversations
WHERE account_a = LEAST(@one::uuid, @other::uuid) AND account_b = GREATEST(@one::uuid, @other::uuid) AND kind = 'pair'`,
		pgx.StrictNamedArgs{"one": one, "other": other}).Scan(&c.ID, &c.AccountA, &c.AccountB, &c.Kind, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Conversation{}, ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("select conversation: %w", err)
	}
	return c, nil
}

// moveLast makes a sent message the conversation's last one when it is the
// newest.
const moveLast = `
UPDATE conversations
SET last_message_id = @id, last_message_at = @at
WHERE id = @conversation AND (last_message_id IS NULL OR last_message_id < @id)`

// written moves the last message for a sent message and, when the message
// became the last one, brings the conversation back from both members'
// archives; it notifies the members the message is visible to either way,
// since the thread changed.
func written(ctx context.Context, tx pgx.Tx, m domain.Message) error {
	var accounts []uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT array_agg(account_id) FROM conversation_members WHERE conversation_id = @id`,
		pgx.StrictNamedArgs{"id": m.ConversationID()}).Scan(&accounts); err != nil {
		return fmt.Errorf("read members: %w", err)
	}
	if m.Status() == domain.StatusSent {
		tag, err := tx.Exec(ctx, moveLast, pgx.StrictNamedArgs{
			"id": m.ID(), "at": m.CreatedAt(), "conversation": m.ConversationID(),
		})
		if err != nil {
			return fmt.Errorf("move the last message: %w", err)
		}
		if tag.RowsAffected() > 0 {
			if _, err = tx.Exec(ctx, `UPDATE conversation_members SET archived_at = NULL WHERE conversation_id = @id`,
				pgx.StrictNamedArgs{"id": m.ConversationID()}); err != nil {
				return fmt.Errorf("unarchive: %w", err)
			}
		}
		// The message's ID is the event's, so writing it sent again inserts
		// no second event. A deal message has none: deal.moved already
		// told whoever hears of the move.
		if m.Deal() == nil {
			if err := events.Insert(ctx, tx, events.Event{
				ID: m.ID(), Kind: events.MessageSent, SubjectID: m.ID(), OccurredAt: m.CreatedAt(),
			}); err != nil {
				return fmt.Errorf("insert message.sent: %w", err)
			}
		}
	} else {
		accounts = []uuid.UUID{m.SenderID()}
	}
	conversation, id := m.ConversationID(), m.ID()
	return stream.Notify(ctx, tx, accounts, stream.Hint{Kind: stream.KindMessage, ConversationID: &conversation, MessageID: &id})
}

func (s pgChats) AddMessage(ctx context.Context, m domain.Message) error {
	st := m.State()
	a := pgx.StrictNamedArgs{
		"id": st.ID, "conversation": st.ConversationID, "sender": st.SenderID, "body": st.Body,
		"photos": photosOf(st), "listing": st.ListingID, "reply_to": st.ReplyTo, "status": st.Status, "at": st.CreatedAt,
		"deal": nil, "move_kind": nil, "move_by": nil, "move_amount": nil,
	}
	if d := st.Deal; d != nil {
		a["deal"], a["move_kind"], a["move_by"] = d.DealID, d.Kind, d.By
		if d.Amount != 0 {
			a["move_amount"] = d.Amount
		}
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO messages (id, conversation_id, sender_id, body, photos, listing_id, reply_to,
                      status, failure, created_at, deal_id, move_kind, move_by, move_amount)
VALUES (@id, @conversation, @sender, @body, @photos, @listing, @reply_to, @status, NULL, @at, @deal, @move_kind,
        @move_by, @move_amount)
ON CONFLICT (id) DO NOTHING`, a)
		if err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		return written(ctx, tx, m)
	})
	if err != nil {
		return fmt.Errorf("add message: %w", err)
	}
	return nil
}

// photosOf is the message's photos as stored, an empty list for none.
func photosOf(st domain.MessageState) []domain.Photo {
	if st.Photos == nil {
		return []domain.Photo{}
	}
	return st.Photos
}

func (s pgChats) DeleteMessage(ctx context.Context, id uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM messages WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	return nil
}

func (s pgChats) GetMessage(ctx context.Context, id uuid.UUID) (domain.Message, error) {
	rows, err := s.db.Query(ctx, `SELECT `+messageColumns+` FROM messages m WHERE m.id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return domain.Message{}, fmt.Errorf("select message: %w", err)
	}
	m, err := pgx.CollectOneRow(rows, scanMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Message{}, ErrNotFound
	}
	if err != nil {
		return domain.Message{}, fmt.Errorf("read message: %w", err)
	}
	return m, nil
}

func (s pgChats) SaveMessage(ctx context.Context, m domain.Message) error {
	st := m.State()
	var failure any
	if st.Failure != "" {
		failure = st.Failure
	}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE messages SET photos = @photos, status = @status, failure = @failure WHERE id = @id`,
			pgx.StrictNamedArgs{"id": st.ID, "photos": photosOf(st), "status": st.Status, "failure": failure}); err != nil {
			return fmt.Errorf("update message: %w", err)
		}
		return written(ctx, tx, m)
	})
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	return nil
}

func (s pgChats) Members(ctx context.Context, conversation uuid.UUID) (map[uuid.UUID]domain.Member, error) {
	rows, err := s.db.Query(ctx, `SELECT account_id, archived_at, last_read_message_id FROM conversation_members
WHERE conversation_id = @conversation`, pgx.StrictNamedArgs{"conversation": conversation})
	if err != nil {
		return nil, fmt.Errorf("select members: %w", err)
	}
	out := map[uuid.UUID]domain.Member{}
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var (
			account uuid.UUID
			m       domain.Member
		)
		if scanErr := row.Scan(&account, &m.ArchivedAt, &m.LastRead); scanErr != nil {
			return struct{}{}, fmt.Errorf("scan member: %w", scanErr)
		}
		out[account] = m
		return struct{}{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read members: %w", err)
	}
	return out, nil
}

func (s pgChats) MarkRead(ctx context.Context, conversation, account, message, other uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var moved, cleared bool
		err := tx.QueryRow(ctx, `UPDATE conversation_members
SET last_read_message_id = CASE
        WHEN last_read_message_id IS NULL OR last_read_message_id < @message THEN @message
        ELSE last_read_message_id END,
    marked_unread = false
WHERE conversation_id = @conversation AND account_id = @account
RETURNING old.last_read_message_id IS DISTINCT FROM new.last_read_message_id, old.marked_unread`,
			pgx.StrictNamedArgs{"message": message, "conversation": conversation, "account": account}).Scan(&moved, &cleared)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("update marker: %w", err)
		}
		var to []uuid.UUID
		if moved {
			to = append(to, other)
		}
		if cleared {
			to = append(to, account)
		}
		if len(to) == 0 {
			return nil
		}
		return stream.Notify(ctx, tx, to, stream.Hint{Kind: stream.KindRead, ConversationID: &conversation})
	})
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}

func (s pgChats) MarkUnread(ctx context.Context, conversation, account uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE conversation_members SET marked_unread = true
WHERE conversation_id = @conversation AND account_id = @account AND NOT marked_unread`,
			pgx.StrictNamedArgs{"conversation": conversation, "account": account})
		if err != nil {
			return fmt.Errorf("set the mark: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		return stream.Notify(ctx, tx, []uuid.UUID{account}, stream.Hint{Kind: stream.KindRead, ConversationID: &conversation})
	})
	if err != nil {
		return fmt.Errorf("mark unread: %w", err)
	}
	return nil
}

func (s pgChats) SetArchived(ctx context.Context, conversation, account uuid.UUID, at *time.Time) error {
	if _, err := s.db.Exec(ctx, `UPDATE conversation_members SET archived_at = @at
WHERE conversation_id = @conversation AND account_id = @account`,
		pgx.StrictNamedArgs{"at": at, "conversation": conversation, "account": account}); err != nil {
		return fmt.Errorf("set archived: %w", err)
	}
	return nil
}

func (s pgChats) Messages(ctx context.Context, conversation, viewer uuid.UUID, p page.Request) ([]domain.Message, error) {
	rows, err := s.db.Query(ctx, `SELECT `+messageColumns+` FROM messages m
WHERE m.conversation_id = @conversation AND (m.status = 'sent' OR m.sender_id = @viewer) AND m.id < @before
ORDER BY m.id DESC LIMIT @limit`, pgx.StrictNamedArgs{
		"conversation": conversation, "viewer": viewer, "before": p.Before(), "limit": p.Fetch(),
	})
	if err != nil {
		return nil, fmt.Errorf("select messages: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanMessage)
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}
	return out, nil
}

func (s pgChats) Window(ctx context.Context, conversation, upTo uuid.UUID, n int) ([]domain.Message, error) {
	if upTo == (uuid.UUID{}) {
		upTo = uuid.Max()
	}
	rows, err := s.db.Query(ctx, `SELECT `+messageColumns+` FROM messages m
WHERE m.conversation_id = @conversation AND m.status = 'sent' AND m.body <> '' AND m.id <= @up_to
ORDER BY m.id DESC LIMIT @limit`, pgx.StrictNamedArgs{"conversation": conversation, "up_to": upTo, "limit": n})
	if err != nil {
		return nil, fmt.Errorf("select window: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanMessage)
	if err != nil {
		return nil, fmt.Errorf("read window: %w", err)
	}
	return out, nil
}

// listed is the condition for a conversation of the viewer's member row cm
// to show in the inbox: a sent message, archived or not as @archived asks,
// and no block of the other member by the viewer.
const listed = `
cm.account_id = @viewer AND c.kind = 'pair' AND c.last_message_id IS NOT NULL
  AND @archived = (cm.archived_at IS NOT NULL)
  AND ` + notBlocked

// notBlocked holds for a conversation c of the viewer's when the viewer
// does not block its other member.
const notBlocked = `NOT EXISTS (
      SELECT 1 FROM account_blocks b
      WHERE b.blocker_id = @viewer
        AND b.blocked_id = CASE WHEN c.account_a = @viewer THEN c.account_b ELSE c.account_a END
  )`

// unreadAfter counts the other member's sent messages in conversation c
// after marker last_read, as a range on messages_conversation.
const unreadAfter = `
CROSS JOIN LATERAL (
    SELECT count(*) AS n FROM messages u
    WHERE u.conversation_id = c.id
      AND u.id > coalesce(last_read, '00000000-0000-0000-0000-000000000000'::uuid)
      AND u.status = 'sent' AND u.sender_id <> @viewer
) unread`

// rowUnread is a row's unread count: the messages after the marker, and at
// least 1 while the member's unread mark is set. It reads marked from the
// query's member row.
const rowUnread = `greatest(unread.n, marked::int)`

// selectInbox picks the page first, then reads each row's last message and
// unread count.
const selectInbox = `
SELECT ` + messageColumns + `, c.id, c.account_a, c.account_b, c.kind, c.created_at, ` + rowUnread + `
FROM (
    SELECT c.id, cm.last_read_message_id AS last_read, cm.marked_unread AS marked
    FROM conversation_members cm JOIN conversations c ON c.id = cm.conversation_id
    WHERE ` + listed + ` AND c.last_message_id < @before
    ORDER BY c.last_message_id DESC LIMIT @limit
) page
JOIN conversations c ON c.id = page.id
JOIN messages m ON m.id = c.last_message_id` + unreadAfter + `
ORDER BY c.last_message_id DESC`

const countUnread = `
SELECT coalesce(sum(` + rowUnread + `), 0)
FROM conversation_members cm JOIN conversations c ON c.id = cm.conversation_id
CROSS JOIN LATERAL (SELECT cm.last_read_message_id, cm.marked_unread) marker (last_read, marked)` + unreadAfter + `
WHERE ` + listed

// countSupportUnread sums the viewer's unread messages over its support
// chats.
const countSupportUnread = `
SELECT coalesce(sum(` + rowUnread + `), 0)
FROM conversation_members cm JOIN conversations c ON c.id = cm.conversation_id
CROSS JOIN LATERAL (SELECT cm.last_read_message_id, cm.marked_unread) marker (last_read, marked)` + unreadAfter + `
WHERE cm.account_id = @viewer AND c.kind = 'support' AND c.last_message_id IS NOT NULL`

// selectThreads reads the last message and unread count of each of the
// viewer's conversations among @ids that has a message.
const selectThreads = `
SELECT ` + messageColumns + `, c.id, ` + rowUnread + `
FROM conversation_members cm JOIN conversations c ON c.id = cm.conversation_id
JOIN messages m ON m.id = c.last_message_id
CROSS JOIN LATERAL (SELECT cm.last_read_message_id, cm.marked_unread) marker (last_read, marked)` + unreadAfter + `
WHERE cm.account_id = @viewer AND c.id = ANY(@ids)`

func (s pgChats) OpenSupport(ctx context.Context, c domain.Conversation) (domain.Conversation, error) {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, kind, created_at)
VALUES (@id, @a, @b, 'support', @at) ON CONFLICT (id) DO NOTHING`,
			pgx.StrictNamedArgs{"id": c.ID, "a": c.AccountA, "b": c.AccountB, "at": c.CreatedAt}); err != nil {
			return fmt.Errorf("insert conversation: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conversation_members (conversation_id, account_id)
VALUES (@id, @a), (@id, @b) ON CONFLICT DO NOTHING`, pgx.StrictNamedArgs{"id": c.ID, "a": c.AccountA, "b": c.AccountB}); err != nil {
			return fmt.Errorf("insert members: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("open support conversation: %w", err)
	}
	return s.Get(ctx, c.ID)
}

func (s pgChats) SupportUnread(ctx context.Context, viewer uuid.UUID) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, countSupportUnread, pgx.StrictNamedArgs{"viewer": viewer}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count support unread: %w", err)
	}
	return n, nil
}

// selectFirsts reads the oldest sent message of each conversation among
// ids, which Threads has already limited to the viewer's.
const selectFirsts = `
SELECT DISTINCT ON (m.conversation_id) ` + messageColumns + `
FROM messages m WHERE m.conversation_id = ANY(@ids) AND m.status = 'sent'
ORDER BY m.conversation_id, m.id`

func (s pgChats) Threads(ctx context.Context, viewer uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]ThreadRow, error) {
	rows, err := s.db.Query(ctx, selectThreads, pgx.StrictNamedArgs{"viewer": viewer, "ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select threads: %w", err)
	}
	out := map[uuid.UUID]ThreadRow{}
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var (
			m    messageScan
			id   uuid.UUID
			r    ThreadRow
			last domain.Message
		)
		if scanErr := row.Scan(append(m.dest(), &id, &r.Unread)...); scanErr != nil {
			return struct{}{}, fmt.Errorf("scan thread: %w", scanErr)
		}
		last, scanErr := m.message()
		r.Last = last
		out[id] = r
		return struct{}{}, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("read threads: %w", err)
	}
	mine := make([]uuid.UUID, 0, len(out))
	for id := range out {
		mine = append(mine, id)
	}
	rows, err = s.db.Query(ctx, selectFirsts, pgx.StrictNamedArgs{"ids": mine})
	if err != nil {
		return nil, fmt.Errorf("select first messages: %w", err)
	}
	firsts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Message, error) {
		var m messageScan
		if scanErr := row.Scan(m.dest()...); scanErr != nil {
			return domain.Message{}, fmt.Errorf("scan first message: %w", scanErr)
		}
		return m.message()
	})
	if err != nil {
		return nil, fmt.Errorf("read first messages: %w", err)
	}
	for i := range firsts {
		id := firsts[i].ConversationID()
		r := out[id]
		r.First = firsts[i]
		out[id] = r
	}
	return out, nil
}

func (s pgChats) Inbox(ctx context.Context, viewer uuid.UUID, archived bool, p page.Request) ([]InboxRow, error) {
	rows, err := s.db.Query(ctx, selectInbox, pgx.StrictNamedArgs{
		"viewer": viewer, "archived": archived, "before": p.Before(), "limit": p.Fetch(),
	})
	if err != nil {
		return nil, fmt.Errorf("select inbox: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (InboxRow, error) {
		var (
			r InboxRow
			m messageScan
		)
		if scanErr := row.Scan(append(m.dest(), &r.Conversation.ID, &r.Conversation.AccountA, &r.Conversation.AccountB,
			&r.Conversation.Kind, &r.Conversation.CreatedAt, &r.Unread)...); scanErr != nil {
			return InboxRow{}, fmt.Errorf("scan inbox row: %w", scanErr)
		}
		r.Other, r.Archived = r.Conversation.Other(viewer), archived
		last, msgErr := m.message()
		r.Last = last
		return r, msgErr
	})
	if err != nil {
		return nil, fmt.Errorf("read inbox: %w", err)
	}
	return out, nil
}

func (s pgChats) Unread(ctx context.Context, viewer uuid.UUID) (int, error) {
	var n int
	if err := s.db.QueryRow(ctx, countUnread,
		pgx.StrictNamedArgs{"viewer": viewer, "archived": false}).Scan(&n); err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}
