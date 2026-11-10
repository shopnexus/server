package repository

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// splitter is the messages_search index's tokenizer (migration 0031);
// queries and names are split with the same one.
const splitter = `pdb.simple('ascii_folding=true')`

func (s pgChats) Tokens(ctx context.Context, q string) ([]string, error) {
	var out []string
	if err := s.db.QueryRow(ctx, `SELECT @q::`+splitter+`::text[]`, pgx.StrictNamedArgs{"q": q}).Scan(&out); err != nil {
		return nil, fmt.Errorf("split query: %w", err)
	}
	return out, nil
}

// matches holds when the tokens of the text expression hold every whole
// term and a word starting with the prefix.
func matches(expr string) string {
	toks := `(` + expr + `)::` + splitter + `::text[]`
	return `(` + toks + ` @> @whole AND EXISTS (SELECT 1 FROM unnest(` + toks + `) w WHERE starts_with(w, @prefix)))`
}

func (s pgChats) SearchThreads(ctx context.Context, viewer uuid.UUID, t Terms, limit int) ([]InboxRow, error) {
	// The page of matching conversations comes first, then each row's last
	// message and unread count, as selectInbox reads them.
	rows, err := s.db.Query(ctx, `
SELECT `+messageColumns+`, c.id, c.account_a, c.account_b, c.created_at, `+rowUnread+`, page.archived
FROM (
    SELECT c.id, cm.last_read_message_id AS last_read, cm.marked_unread AS marked, cm.archived_at IS NOT NULL AS archived
    FROM conversation_members cm JOIN conversations c ON c.id = cm.conversation_id
    JOIN accounts o ON o.id = CASE WHEN c.account_a = @viewer THEN c.account_b ELSE c.account_a END
    WHERE cm.account_id = @viewer AND c.last_message_id IS NOT NULL AND `+notBlocked+`
      AND (`+matches(`coalesce(o.display_name, '')`)+` OR EXISTS (
          SELECT 1 FROM messages lm JOIN listings l ON l.id = lm.listing_id
          WHERE lm.conversation_id = c.id AND `+matches(`l.title`)+`))
    ORDER BY c.last_message_id DESC LIMIT @limit
) page
JOIN conversations c ON c.id = page.id
JOIN messages m ON m.id = c.last_message_id`+unreadAfter+`
ORDER BY c.last_message_id DESC`, pgx.StrictNamedArgs{
		"viewer": viewer, "whole": t.Whole, "prefix": t.Prefix, "limit": limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search threads: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (InboxRow, error) {
		var (
			r InboxRow
			m messageScan
		)
		if scanErr := row.Scan(append(m.dest(), &r.Conversation.ID, &r.Conversation.AccountA, &r.Conversation.AccountB,
			&r.Conversation.CreatedAt, &r.Unread, &r.Archived)...); scanErr != nil {
			return InboxRow{}, fmt.Errorf("scan thread: %w", scanErr)
		}
		r.Other = r.Conversation.Other(viewer)
		last, msgErr := m.message()
		r.Last = last
		return r, msgErr
	})
	if err != nil {
		return nil, fmt.Errorf("read threads: %w", err)
	}
	return out, nil
}

func (s pgChats) SearchMessages(ctx context.Context, viewer uuid.UUID, t Terms, p page.Request) ([]MessageHitRow, error) {
	// One regex clause per token keeps the whole query inside the index: a
	// regex matches a whole term, and the prefix's ".*" expands without
	// phrase_prefix's cap of 50 terms.
	args := pgx.StrictNamedArgs{
		"viewer": viewer, "before": p.Before(), "limit": p.Fetch(), "prefix": regexp.QuoteMeta(t.Prefix) + ".*",
	}
	clauses := make([]string, 0, 1+len(t.Whole))
	clauses = append(clauses, `m.body @@@ pdb.regex(@prefix)`)
	for i, w := range t.Whole {
		name := "w" + strconv.Itoa(i)
		args[name] = regexp.QuoteMeta(w)
		clauses = append(clauses, `m.body @@@ pdb.regex(@`+name+`)`)
	}
	rows, err := s.db.Query(ctx, `SELECT `+messageColumns+` FROM messages m
WHERE `+strings.Join(clauses, " AND ")+`
  AND m.status = 'sent'
  AND m.conversation_id = ANY(ARRAY(
      SELECT c.id FROM conversation_members cm JOIN conversations c ON c.id = cm.conversation_id
      WHERE cm.account_id = @viewer AND c.last_message_id IS NOT NULL AND `+notBlocked+`))
  AND m.id < @before
ORDER BY m.id DESC LIMIT @limit`, args)
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	msgs, err := pgx.CollectRows(rows, scanMessage)
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(msgs))
	for i := range msgs {
		ids = append(ids, msgs[i].ConversationID())
	}
	convs, err := s.db.Query(ctx, `SELECT id, account_a, account_b, created_at FROM conversations WHERE id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("read conversations: %w", err)
	}
	pairs, err := pgx.CollectRows(convs, func(row pgx.CollectableRow) (domain.Conversation, error) {
		var c domain.Conversation
		return c, row.Scan(&c.ID, &c.AccountA, &c.AccountB, &c.CreatedAt)
	})
	if err != nil {
		return nil, fmt.Errorf("read conversations: %w", err)
	}
	byID := make(map[uuid.UUID]domain.Conversation, len(pairs))
	for _, c := range pairs {
		byID[c.ID] = c
	}
	out := make([]MessageHitRow, len(msgs))
	for i := range msgs {
		out[i] = MessageHitRow{Message: msgs[i], Other: byID[msgs[i].ConversationID()].Other(viewer)}
	}
	return out, nil
}
