-- The console agent (2026-11-17): what it read of people for which member,
-- and the seller of each order's ticket.
--
-- console_agent_reads records every read of personal data (messages,
-- addresses, phone numbers and emails, carrier proof) an MCP tool made for
-- a member: the tool, what it read (a ticket, an account) and the ids it
-- returned. Rows are deleted 90 days after read_at, hourly, so the index
-- on read_at serves the sweep.
CREATE TABLE console_agent_reads (
    id           uuid PRIMARY KEY,
    member_id    uuid NOT NULL REFERENCES console_members (id),
    tool_name    text NOT NULL,
    subject_kind text NOT NULL CHECK (subject_kind IN ('ticket', 'account')),
    subject_ids  uuid[] NOT NULL CHECK (cardinality(subject_ids) > 0),
    read_at      timestamptz NOT NULL
);

CREATE INDEX console_agent_reads_read_at ON console_agent_reads (read_at);

-- A not_received ticket keeps its order's seller, so the tickets of an
-- account are those it opened and those on orders it sold.
ALTER TABLE tickets ADD COLUMN seller_id uuid REFERENCES accounts (id);
UPDATE tickets t SET seller_id = o.seller_id FROM orders AS o WHERE t.order_id = o.id;
ALTER TABLE tickets ADD CONSTRAINT tickets_seller CHECK ((order_id IS NULL) = (seller_id IS NULL));
CREATE INDEX tickets_seller ON tickets (seller_id, opened_at) WHERE seller_id IS NOT NULL;
