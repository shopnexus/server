package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/stream"
)

type pgChats struct{ db *pgxpool.Pool }

func NewChats(db *pgxpool.Pool) Chats { return pgChats{db} }

const messageColumns = `m.id, m.conversation_id, m.sender_id, m.body, m.photos, m.listing_id, m.status, m.failure,
       m.created_at`

func scanMessage(row pgx.Row, extra ...any) (domain.Message, error) {
	var (
		st      domain.MessageState
		photos  []byte
		failure *string
	)
	dest := append([]any{
		&st.ID, &st.ConversationID, &st.SenderID, &st.Body, &photos, &st.ListingID, &st.Status,
		&failure, &st.CreatedAt,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return domain.Message{}, fmt.Errorf("scan message: %w", err)
	}
	if err := json.Unmarshal(photos, &st.Photos); err != nil {
		return domain.Message{}, fmt.Errorf("decode photos: %w", err)
	}
	if failure != nil {
		st.Failure = *failure
	}
	return domain.RestoreMessage(st), nil
}

func (s pgChats) OpenPair(ctx context.Context, c domain.Conversation) (domain.Conversation, error) {
	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, created_at)
VALUES (@id, @a, @b, @at) ON CONFLICT (account_a, account_b) DO NOTHING`,
			pgx.StrictNamedArgs{"id": c.ID, "a": c.AccountA, "b": c.AccountB, "at": c.CreatedAt}); err != nil {
			return fmt.Errorf("insert conversation: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT id, account_a, account_b, created_at FROM conversations
WHERE account_a = @a AND account_b = @b`, pgx.StrictNamedArgs{"a": c.AccountA, "b": c.AccountB}).
			Scan(&out.ID, &out.AccountA, &out.AccountB, &out.CreatedAt); err != nil {
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

func (s pgChats) Get(ctx context.Context, id uuid.UUID) (domain.Conversation, error) {
	var c domain.Conversation
	err := s.db.QueryRow(ctx, `SELECT id, account_a, account_b, created_at FROM conversations WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}).Scan(&c.ID, &c.AccountA, &c.AccountB, &c.CreatedAt)
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
	err := s.db.QueryRow(ctx, `SELECT id, account_a, account_b, created_at FROM conversations
WHERE account_a = LEAST(@one::uuid, @other::uuid) AND account_b = GREATEST(@one::uuid, @other::uuid)`,
		pgx.StrictNamedArgs{"one": one, "other": other}).Scan(&c.ID, &c.AccountA, &c.AccountB, &c.CreatedAt)
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

// written moves the last message for a sent message, brings the
// conversation back from both members' archives, and notifies the members
// it is visible to.
func written(ctx context.Context, tx pgx.Tx, m domain.Message) error {
	var accounts []uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT array_agg(account_id) FROM conversation_members WHERE conversation_id = @id`,
		pgx.StrictNamedArgs{"id": m.ConversationID()}).Scan(&accounts); err != nil {
		return fmt.Errorf("read members: %w", err)
	}
	if m.Status() == domain.StatusSent {
		if _, err := tx.Exec(ctx, moveLast, pgx.StrictNamedArgs{
			"id": m.ID(), "at": m.CreatedAt(), "conversation": m.ConversationID(),
		}); err != nil {
			return fmt.Errorf("move the last message: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE conversation_members SET archived_at = NULL WHERE conversation_id = @id`,
			pgx.StrictNamedArgs{"id": m.ConversationID()}); err != nil {
			return fmt.Errorf("unarchive: %w", err)
		}
	} else {
		accounts = []uuid.UUID{m.SenderID()}
	}
	conversation, id := m.ConversationID(), m.ID()
	return stream.Notify(ctx, tx, accounts, stream.Hint{Kind: stream.KindMessage, ConversationID: &conversation, MessageID: &id})
}

func (s pgChats) AddMessage(ctx context.Context, m domain.Message) error {
	st := m.State()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO messages (id, conversation_id, sender_id, body, photos, listing_id, status,
                      failure, created_at)
VALUES (@id, @conversation, @sender, @body, @photos, @listing, @status, NULL, @at)
ON CONFLICT (id) DO NOTHING`, pgx.StrictNamedArgs{
			"id": st.ID, "conversation": st.ConversationID, "sender": st.SenderID, "body": st.Body,
			"photos": photosOf(st), "listing": st.ListingID, "status": st.Status, "at": st.CreatedAt,
		})
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
	m, err := scanMessage(s.db.QueryRow(ctx, `SELECT `+messageColumns+` FROM messages m WHERE m.id = @id`,
		pgx.StrictNamedArgs{"id": id}))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Message{}, ErrNotFound
	}
	if err != nil {
		return domain.Message{}, err
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

func (s pgChats) Member(ctx context.Context, conversation, account uuid.UUID) (domain.Member, error) {
	m := domain.Member{ConversationID: conversation, AccountID: account}
	var lastRead *uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT last_read_message_id, archived_at FROM conversation_members
WHERE conversation_id = @conversation AND account_id = @account`,
		pgx.StrictNamedArgs{"conversation": conversation, "account": account}).Scan(&lastRead, &m.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, ErrNotFound
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("select member: %w", err)
	}
	if lastRead != nil {
		m.LastRead = *lastRead
	}
	return m, nil
}

func (s pgChats) MarkRead(ctx context.Context, conversation, account, message, other uuid.UUID) (bool, error) {
	moved := false
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE conversation_members SET last_read_message_id = @message
WHERE conversation_id = @conversation AND account_id = @account
  AND (last_read_message_id IS NULL OR last_read_message_id < @message)`,
			pgx.StrictNamedArgs{"message": message, "conversation": conversation, "account": account})
		if err != nil {
			return fmt.Errorf("update marker: %w", err)
		}
		if moved = tag.RowsAffected() == 1; !moved {
			return nil
		}
		return stream.Notify(ctx, tx, []uuid.UUID{other}, stream.Hint{Kind: stream.KindRead, ConversationID: &conversation})
	})
	if err != nil {
		return false, fmt.Errorf("mark read: %w", err)
	}
	return moved, nil
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
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		m, scanErr := scanMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}
	return out, nil
}

// listed is the condition for a conversation of the viewer's member row cm
// to show in the inbox: a sent message, archived or not as @archived asks,
// and no block of the other member by the viewer.
const listed = `
cm.account_id = @viewer AND c.last_message_id IS NOT NULL
  AND @archived = (cm.archived_at IS NOT NULL)
  AND NOT EXISTS (
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

// selectInbox picks the page first, then reads each row's last message and
// unread count.
const selectInbox = `
SELECT ` + messageColumns + `, c.id, c.account_a, c.account_b, c.created_at, unread.n
FROM (
    SELECT c.id, cm.last_read_message_id AS last_read
    FROM conversation_members cm JOIN conversations c ON c.id = cm.conversation_id
    WHERE ` + listed + ` AND c.last_message_id < @before
    ORDER BY c.last_message_id DESC LIMIT @limit
) page
JOIN conversations c ON c.id = page.id
JOIN messages m ON m.id = c.last_message_id` + unreadAfter + `
ORDER BY c.last_message_id DESC`

const countUnread = `
SELECT coalesce(sum(unread.n), 0)
FROM conversation_members cm JOIN conversations c ON c.id = cm.conversation_id
CROSS JOIN LATERAL (SELECT cm.last_read_message_id) marker (last_read)` + unreadAfter + `
WHERE ` + listed

func (s pgChats) Inbox(ctx context.Context, viewer uuid.UUID, archived bool, p page.Request) ([]InboxRow, error) {
	rows, err := s.db.Query(ctx, selectInbox, pgx.StrictNamedArgs{
		"viewer": viewer, "archived": archived, "before": p.Before(), "limit": p.Fetch(),
	})
	if err != nil {
		return nil, fmt.Errorf("select inbox: %w", err)
	}
	defer rows.Close()
	var out []InboxRow
	for rows.Next() {
		var r InboxRow
		r.Last, err = scanMessage(rows, &r.Conversation.ID, &r.Conversation.AccountA, &r.Conversation.AccountB,
			&r.Conversation.CreatedAt, &r.Unread)
		if err != nil {
			return nil, err
		}
		r.Other = r.Conversation.Other(viewer)
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
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
