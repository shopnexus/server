package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/chat/domain"
)

// replyThreads takes the first sent message of each of the seller's pair
// conversations with a message since since, through messages_conversation,
// and keeps those another person wrote about a listing of the seller's:
// the listing the message names, or, when it names none, a deal in the
// conversation where the seller sells. Opening a deal opens the
// conversation, so a buyer's first message there often names no listing.
// The listing join follows SearchThreads'. Deal messages are moves, not
// answers, so neither side's count reads them.
const replyThreads = `
SELECT f.created_at,
       (SELECT r.created_at FROM messages r
        WHERE r.conversation_id = c.id AND r.sender_id = @seller AND r.status = 'sent' AND r.deal_id IS NULL
        ORDER BY r.id LIMIT 1)
FROM conversation_members cm
JOIN conversations c ON c.id = cm.conversation_id
CROSS JOIN LATERAL (
    SELECT m.sender_id, m.listing_id, m.created_at FROM messages m
    WHERE m.conversation_id = c.id AND m.status = 'sent' AND m.deal_id IS NULL
    ORDER BY m.id LIMIT 1
) f
LEFT JOIN listings l ON l.id = f.listing_id
WHERE cm.account_id = @seller AND c.kind = 'pair' AND c.last_message_at >= @since
  AND f.created_at >= @since AND f.sender_id <> @seller
  AND (l.seller_id = @seller OR (f.listing_id IS NULL AND EXISTS (
      SELECT 1 FROM deals d WHERE d.conversation_id = c.id AND d.seller_id = @seller
  )))`

func (s pgChats) ReplyThreads(ctx context.Context, seller uuid.UUID, since time.Time) ([]domain.ReplyThread, error) {
	rows, err := s.db.Query(ctx, replyThreads, pgx.StrictNamedArgs{"seller": seller, "since": since})
	if err != nil {
		return nil, fmt.Errorf("select reply threads: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.ReplyThread])
	if err != nil {
		return nil, fmt.Errorf("read reply threads: %w", err)
	}
	return out, nil
}

// listingSenders reads through messages_listing. A message names a listing
// of one of its conversation's members, so a sender other than the seller
// in the seller's pair conversation is the other member, asking.
const listingSenders = `
SELECT m.listing_id, m.sender_id, min(m.created_at) AS first
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE m.listing_id = ANY(@ids) AND m.status = 'sent' AND m.deal_id IS NULL AND m.sender_id <> @seller
  AND c.kind = 'pair' AND @seller IN (c.account_a, c.account_b)
GROUP BY m.listing_id, m.sender_id
ORDER BY first`

func (s pgChats) ListingSenders(ctx context.Context, seller uuid.UUID, ids []uuid.UUID) ([]ListingSender, error) {
	rows, err := s.db.Query(ctx, listingSenders, pgx.StrictNamedArgs{"seller": seller, "ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select listing senders: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ListingSender])
	if err != nil {
		return nil, fmt.Errorf("read listing senders: %w", err)
	}
	return out, nil
}
