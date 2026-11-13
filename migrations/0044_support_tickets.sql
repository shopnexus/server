-- Support tickets (2026-11-13): each ticket is a chat between a person and
-- the system account ShopNexus, which has no identity, so nobody signs in
-- to it.
INSERT INTO accounts (id, display_name, status, write_id, created_at, updated_at)
VALUES ('0193e000-0000-7000-8000-000000000001', 'ShopNexus', 'active', uuidv7(), now(), now());

-- A pair conversation is the one chat of two people; a support one is one
-- ticket's chat, and a person has as many as tickets.
ALTER TABLE conversations ADD COLUMN kind text NOT NULL DEFAULT 'pair' CHECK (kind IN ('pair', 'support'));
ALTER TABLE conversations ALTER COLUMN kind DROP DEFAULT;
ALTER TABLE conversations DROP CONSTRAINT conversations_account_a_account_b_key;
CREATE UNIQUE INDEX conversations_pair ON conversations (account_a, account_b) WHERE kind = 'pair';

-- A message may quote an earlier sent message of its conversation.
ALTER TABLE messages ADD COLUMN reply_to uuid REFERENCES messages (id);

-- Who completed an order: the buyer's receipt, or the system after the
-- carrier reported it delivered. An order from before the moves were
-- written counts as the buyer's.
ALTER TABLE orders ADD COLUMN completed_by text CHECK (completed_by IN ('buyer', 'system'));
UPDATE orders o SET completed_by = coalesce((
    SELECT m.by_party FROM deal_moves m
    WHERE m.deal_id = o.deal_id AND m.kind = 'completed' AND m.by_party IN ('buyer', 'system')
    ORDER BY m.moved_at DESC LIMIT 1
), 'buyer')
WHERE o.status = 'completed';
ALTER TABLE orders ADD CONSTRAINT orders_completed_by CHECK ((status = 'completed') = (completed_by IS NOT NULL));

-- last_from is who wrote last: person asks for an answer. carrier_key is
-- the carrier's name folded, for counting requests of one carrier.
CREATE TABLE tickets (
    id               uuid PRIMARY KEY,
    account_id       uuid NOT NULL REFERENCES accounts (id),
    conversation_id  uuid NOT NULL UNIQUE REFERENCES conversations (id),
    kind             text NOT NULL CHECK (kind IN ('not_received', 'carrier_request', 'other')),
    status           text NOT NULL CHECK (status IN ('open', 'resolved')),
    order_id         uuid REFERENCES orders (id),
    carrier_name     text CHECK (char_length(carrier_name) BETWEEN 1 AND 50),
    carrier_key      text,
    first_message_id uuid REFERENCES messages (id),
    last_message_at  timestamptz NOT NULL,
    last_from        text NOT NULL CHECK (last_from IN ('person', 'shopnexus')),
    opened_at        timestamptz NOT NULL,
    resolved_at      timestamptz,
    resolved_by      uuid REFERENCES console_members (id),
    request_key      uuid NOT NULL,
    UNIQUE (account_id, request_key),
    CHECK ((kind = 'not_received') = (order_id IS NOT NULL)),
    CHECK ((kind = 'carrier_request') = (carrier_name IS NOT NULL AND carrier_key IS NOT NULL)),
    CHECK ((status = 'resolved') = (resolved_at IS NOT NULL))
);
CREATE UNIQUE INDEX tickets_one_per_order ON tickets (order_id) WHERE kind = 'not_received';
CREATE INDEX tickets_account ON tickets (account_id, opened_at);
CREATE INDEX tickets_queue ON tickets (status, last_message_at DESC, id DESC);
CREATE INDEX tickets_carrier ON tickets (carrier_key, opened_at) WHERE kind = 'carrier_request';

CREATE TABLE ticket_events (
    id        uuid PRIMARY KEY,
    ticket_id uuid NOT NULL REFERENCES tickets (id),
    kind      text NOT NULL CHECK (kind IN ('opened', 'resolved', 'reopened')),
    member_id uuid REFERENCES console_members (id),
    at        timestamptz NOT NULL
);
CREATE INDEX ticket_events_ticket ON ticket_events (ticket_id, at);

-- Who wrote each ShopNexus message of a ticket; member_id is null for one
-- ShopNexus sent by itself.
CREATE TABLE ticket_replies (
    message_id uuid PRIMARY KEY REFERENCES messages (id),
    ticket_id  uuid NOT NULL REFERENCES tickets (id),
    member_id  uuid REFERENCES console_members (id)
);

ALTER TABLE console_actions DROP CONSTRAINT console_actions_target_kind_check;
ALTER TABLE console_actions ADD CONSTRAINT console_actions_target_kind_check
CHECK (target_kind IN ('account', 'member', 'ticket'));

-- Feedback becomes open tickets of kind other, its text the first message.
CREATE TEMPORARY TABLE moved ON COMMIT DROP AS
SELECT f.account_id, f.body, f.request_key, f.created_at,
       uuidv7() AS conversation_id, uuidv7() AS message_id, uuidv7() AS ticket_id
FROM feedback f;

INSERT INTO conversations (id, account_a, account_b, kind, created_at)
SELECT conversation_id,
       LEAST(account_id, '0193e000-0000-7000-8000-000000000001'::uuid),
       GREATEST(account_id, '0193e000-0000-7000-8000-000000000001'::uuid),
       'support', created_at
FROM moved;

INSERT INTO conversation_members (conversation_id, account_id)
SELECT conversation_id, account_id FROM moved
UNION ALL
SELECT conversation_id, '0193e000-0000-7000-8000-000000000001'::uuid FROM moved;

INSERT INTO messages (id, conversation_id, sender_id, body, photos, status, created_at)
SELECT message_id, conversation_id, account_id, body, '[]', 'sent', created_at FROM moved;

UPDATE conversations c SET last_message_id = m.message_id, last_message_at = m.created_at
FROM moved m WHERE c.id = m.conversation_id;

INSERT INTO tickets (id, account_id, conversation_id, kind, status, first_message_id, last_message_at, last_from,
                     opened_at, request_key)
SELECT ticket_id, account_id, conversation_id, 'other', 'open', message_id, created_at, 'person', created_at, request_key
FROM moved;

INSERT INTO ticket_events (id, ticket_id, kind, at)
SELECT uuidv7(), ticket_id, 'opened', created_at FROM moved;

DROP TABLE feedback;
