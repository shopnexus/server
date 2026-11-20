-- Trust signals (2026-11-20): photos and helpful marks on reviews, and the
-- answers that give a seller's usual reply time.

-- Up to 4 processed photos of a review, [{upload_id, photo_key, thumb_key}].
-- The default serves rows written before the column.
ALTER TABLE reviews ADD COLUMN photos jsonb NOT NULL DEFAULT '[]'::jsonb;

-- One row per person who found a review helpful.
CREATE TABLE review_helpful (
    review_id  uuid NOT NULL REFERENCES reviews (id) ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts (id),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (review_id, account_id)
);

-- One row per message that answered someone who wrote about a listing of
-- its sender's: when the wait began and when the answer came. A sent
-- message writes its own row; rows past 30 days are dropped as the seller
-- answers again.
CREATE TABLE reply_samples (
    message_id  uuid PRIMARY KEY REFERENCES messages (id) ON DELETE CASCADE,
    account_id  uuid NOT NULL REFERENCES accounts (id),
    asked_at    timestamptz NOT NULL,
    answered_at timestamptz NOT NULL
);

CREATE INDEX reply_samples_account ON reply_samples (account_id, answered_at);

-- The answers of the last 30 days, by the rule chat's repository applies to
-- each new message: the wait starts at the other member's first message
-- after the sender's previous one, at least an hour after it.
INSERT INTO reply_samples (message_id, account_id, asked_at, answered_at)
SELECT
    r.id,
    r.sender_id,
    w.asked_at,
    r.created_at
FROM messages AS r
INNER JOIN conversations AS c ON r.conversation_id = c.id
LEFT JOIN LATERAL (
    SELECT
        o.id,
        o.created_at
    FROM messages AS o
    WHERE
        o.conversation_id = c.id AND o.sender_id = r.sender_id AND o.status = 'sent'
        AND o.deal_id IS NULL AND o.id < r.id
    ORDER BY o.id DESC LIMIT 1
) AS own ON TRUE
CROSS JOIN
    LATERAL (
        SELECT min(q.created_at) AS asked_at FROM messages AS q
        WHERE
            q.conversation_id = c.id AND q.sender_id <> r.sender_id AND q.status = 'sent'
            AND q.deal_id IS NULL AND q.id < r.id AND (own.id IS NULL OR q.id > own.id)
    ) AS w
WHERE
    r.status = 'sent' AND r.deal_id IS NULL AND r.created_at >= now() - interval '30 days'
    AND c.kind = 'pair' AND w.asked_at IS NOT NULL
    AND (own.id IS NULL OR w.asked_at >= own.created_at + interval '1 hour')
    AND (
        EXISTS (
            SELECT 1 FROM deals AS d
            WHERE d.conversation_id = c.id AND d.seller_id = r.sender_id
        )
        OR EXISTS (
            SELECT 1 FROM messages AS n INNER JOIN listings AS l ON n.listing_id = l.id
            WHERE n.conversation_id = c.id AND n.sender_id <> r.sender_id AND l.seller_id = r.sender_id
        )
    );
