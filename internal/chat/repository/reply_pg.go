package repository

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/chat/domain"
)

// replyThreads takes the first sent message of each of the seller's pair
// conversations with a message since since, through messages_conversation,
// and keeps those another person wrote about a listing of the seller's;
// the listing join follows SearchThreads'.
const replyThreads = `
SELECT f.created_at,
       (SELECT r.created_at FROM messages r
        WHERE r.conversation_id = c.id AND r.sender_id = @seller AND r.status = 'sent'
        ORDER BY r.id LIMIT 1)
FROM conversation_members cm
JOIN conversations c ON c.id = cm.conversation_id
CROSS JOIN LATERAL (
    SELECT m.sender_id, m.listing_id, m.created_at FROM messages m
    WHERE m.conversation_id = c.id AND m.status = 'sent'
    ORDER BY m.id LIMIT 1
) f
JOIN listings l ON l.id = f.listing_id
WHERE cm.account_id = @seller AND c.kind = 'pair' AND c.last_message_at >= @since
  AND f.created_at >= @since AND f.sender_id <> @seller AND l.seller_id = @seller`

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
