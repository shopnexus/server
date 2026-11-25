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

// replySample records a sent message as its sender's answer when it ends a
// wait in a pair conversation where the sender sells: the other member
// named a listing of the sender's there, or a deal there has the sender
// selling. The wait starts at the other member's first message after the
// sender's previous one, and counts only when that came at least @gap
// seconds after it, or the sender had not written before. Deal messages
// count on neither side, as in replyThreads.
const replySample = `
INSERT INTO reply_samples (message_id, account_id, asked_at, answered_at)
SELECT @id, @sender, w.asked_at, @at
FROM conversations c
LEFT JOIN LATERAL (
    SELECT o.id, o.created_at FROM messages o
    WHERE o.conversation_id = c.id AND o.sender_id = @sender AND o.status = 'sent' AND o.deal_id IS NULL AND o.id < @id
    ORDER BY o.id DESC LIMIT 1
) own ON true
CROSS JOIN LATERAL (
    SELECT min(q.created_at) AS asked_at FROM messages q
    WHERE q.conversation_id = c.id AND q.sender_id <> @sender AND q.status = 'sent' AND q.deal_id IS NULL
      AND q.id < @id AND (own.id IS NULL OR q.id > own.id)
) w
WHERE c.id = @conversation AND c.kind = 'pair' AND w.asked_at IS NOT NULL
  AND (own.id IS NULL OR w.asked_at >= own.created_at + make_interval(secs => @gap))
  AND (EXISTS (SELECT 1 FROM deals d WHERE d.conversation_id = c.id AND d.seller_id = @sender)
       OR EXISTS (SELECT 1 FROM messages n JOIN listings l ON l.id = n.listing_id
                  WHERE n.conversation_id = c.id AND n.sender_id <> @sender AND l.seller_id = @sender))
ON CONFLICT (message_id) DO NOTHING`

// queueSample queues into b m's reply sample, if it is one, and the drop of
// its sender's samples older than domain.ReplyTimePeriod, so the table holds
// the period.
func queueSample(b *pgx.Batch, m domain.Message) {
	b.Queue(replySample, pgx.StrictNamedArgs{
		"id": m.ID(), "sender": m.SenderID(), "conversation": m.ConversationID(), "at": m.CreatedAt(),
		"gap": domain.ReplyGap.Seconds(),
	})
	b.Queue(`DELETE FROM reply_samples WHERE account_id = @sender AND answered_at < @before`,
		pgx.StrictNamedArgs{"sender": m.SenderID(), "before": m.CreatedAt().Add(-domain.ReplyTimePeriod)})
}

func (s pgChats) ReplyWait(ctx context.Context, account uuid.UUID, since time.Time) (samples int, median time.Duration, err error) {
	var seconds float64
	if err = s.db.QueryRow(ctx, `SELECT count(*),
       coalesce(extract(epoch FROM percentile_disc(0.5) WITHIN GROUP (ORDER BY answered_at - asked_at)), 0)::float8
FROM reply_samples WHERE account_id = @account AND answered_at >= @since`,
		pgx.StrictNamedArgs{"account": account, "since": since}).Scan(&samples, &seconds); err != nil {
		return 0, 0, fmt.Errorf("read the reply wait: %w", err)
	}
	return samples, time.Duration(seconds * float64(time.Second)), nil
}
