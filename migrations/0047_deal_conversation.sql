-- A deal lives in the conversation of its buyer and seller. Opening a deal
-- opens the pair's conversation, or reuses it, and the deal keeps its ID.
-- The deals from before this file get their pair's conversation, which is
-- opened here for a pair that had none.
--
-- A deal message is one the server writes for a move on a deal: it names
-- the deal and copies the move's kind, side and amount, and has no text or
-- photos. Its ID is the move's, and its time the move's.

ALTER TABLE deals ADD COLUMN conversation_id uuid REFERENCES conversations (id);

INSERT INTO conversations (id, account_a, account_b, kind, created_at)
SELECT
    uuidv7() AS id,
    pair.account_a,
    pair.account_b,
    'pair' AS kind,
    pair.opened_at
FROM (
    SELECT
        least(buyer_id, seller_id) AS account_a,
        greatest(buyer_id, seller_id) AS account_b,
        min(created_at) AS opened_at
    FROM deals
    GROUP BY least(buyer_id, seller_id), greatest(buyer_id, seller_id)
) AS pair
ON CONFLICT (account_a, account_b) WHERE kind = 'pair' DO NOTHING;

INSERT INTO conversation_members (conversation_id, account_id)
SELECT
    c.id,
    member.account_id
FROM conversations AS c
CROSS JOIN LATERAL (VALUES (c.account_a), (c.account_b)) AS member (account_id)
WHERE c.kind = 'pair'
ON CONFLICT DO NOTHING;

UPDATE deals AS d
SET conversation_id = c.id
FROM conversations AS c
WHERE
    c.kind = 'pair'
    AND c.account_a = least(d.buyer_id, d.seller_id)
    AND c.account_b = greatest(d.buyer_id, d.seller_id);

ALTER TABLE deals ALTER COLUMN conversation_id SET NOT NULL;

ALTER TABLE messages
ADD COLUMN deal_id uuid REFERENCES deals (id),
ADD COLUMN move_kind text,
ADD COLUMN move_by text CHECK (move_by IN ('buyer', 'seller', 'system')),
ADD COLUMN move_amount bigint,
ADD CONSTRAINT messages_deal_move CHECK (
    (deal_id IS NULL) = (move_kind IS NULL)
    AND (deal_id IS NULL) = (move_by IS NULL)
    AND (deal_id IS NOT NULL OR move_amount IS NULL)
    AND (deal_id IS NULL OR (body = '' AND photos = '[]'::jsonb AND status = 'sent'))
);
