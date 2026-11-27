-- The console agent runs in the agent runtime (2026-11-27). A conversation
-- is now the history the model reads, message by message, append-only; a
-- turn is one run of the runtime over it. The agent reads and changes
-- ShopNexus through the console API with a token of its turn, holding its
-- agent's capabilities, and every change it asks for waits for a member's
-- approval.
--
-- The old run tables stay under legacy_ names, their data untouched, until
-- a later migration drops them. Their questions and answers are copied into
-- the new history, so an old conversation reads on.
DO $$
DECLARE r record;
BEGIN
    FOR r IN
        SELECT i.relname AS name FROM pg_index AS x
        JOIN pg_class AS i ON i.oid = x.indexrelid
        JOIN pg_class AS t ON t.oid = x.indrelid
        WHERE t.relnamespace = current_schema()::regnamespace AND t.relname IN (
            'agent_turns', 'agent_turn_inputs', 'agent_tool_calls', 'agent_turn_events', 'agent_documents',
            'agent_document_versions', 'agent_skills'
        )
    LOOP
        EXECUTE format('ALTER INDEX %I RENAME TO %I', r.name, 'legacy_' || r.name);
    END LOOP;
END $$;

ALTER TABLE agent_turns RENAME TO legacy_agent_turns;
ALTER TABLE agent_turn_inputs RENAME TO legacy_agent_turn_inputs;
ALTER TABLE agent_tool_calls RENAME TO legacy_agent_tool_calls;
ALTER TABLE agent_turn_events RENAME TO legacy_agent_turn_events;
ALTER TABLE agent_documents RENAME TO legacy_agent_documents;
ALTER TABLE agent_document_versions RENAME TO legacy_agent_document_versions;
ALTER TABLE agent_skills RENAME TO legacy_agent_skills;

-- agent_turns: one turn of a conversation, run again by each resume.
-- token_hash is the SHA-256 of the run's turn token, NULL once the run
-- ended; capabilities are the agent's when the turn started.
CREATE TABLE agent_turns (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES agent_conversations (id) ON DELETE CASCADE,
    member_id       uuid NOT NULL REFERENCES console_members (id),
    agent_id        uuid NOT NULL,
    agent_name      text NOT NULL,
    model           text NOT NULL,
    capabilities    text[] NOT NULL,
    status          text NOT NULL CHECK (status IN ('running', 'paused', 'stopped', 'succeeded', 'failed', 'timed_out')),
    stop_reason     text,
    error_text      text,
    token_hash      bytea UNIQUE,
    input_tokens    bigint NOT NULL DEFAULT 0,
    output_tokens   bigint NOT NULL DEFAULT 0,
    cached_tokens   bigint NOT NULL DEFAULT 0,
    event_seq       bigint NOT NULL DEFAULT 0,
    request_key     uuid,
    started_at      timestamptz NOT NULL,
    run_started_at  timestamptz NOT NULL,
    deadline_at     timestamptz NOT NULL,
    finished_at     timestamptz,
    CHECK ((status = 'running') = (token_hash IS NOT NULL))
);

CREATE UNIQUE INDEX agent_turns_running ON agent_turns (conversation_id) WHERE status = 'running';
CREATE INDEX agent_turns_conversation ON agent_turns (conversation_id, started_at);
CREATE INDEX agent_turns_deadline ON agent_turns (deadline_at) WHERE status = 'running';
CREATE UNIQUE INDEX agent_turns_member_request_key ON agent_turns (member_id, request_key)
WHERE request_key IS NOT NULL;

-- agent_messages: a conversation's history as the model reads it, one
-- agent message (role, context, content, tool calls, ...) per row, in seq
-- order. Rows are only added, so every request a turn sends is the one
-- before it plus what is new, which the prompt cache depends on.
CREATE TABLE agent_messages (
    conversation_id uuid NOT NULL REFERENCES agent_conversations (id) ON DELETE CASCADE,
    seq             integer NOT NULL,
    turn_id         uuid REFERENCES agent_turns (id) ON DELETE SET NULL,
    message         jsonb NOT NULL,
    created_at      timestamptz NOT NULL,
    PRIMARY KEY (conversation_id, seq)
);

CREATE INDEX agent_messages_turn ON agent_messages (turn_id);
CREATE INDEX agent_messages_tool_kept ON agent_messages (created_at)
WHERE message ->> 'role' = 'tool' AND message ->> 'content' <> '';

-- agent_turn_events: a turn's events as the console streams them; seq
-- counts from 1 per turn and comes from agent_turns.event_seq.
CREATE TABLE agent_turn_events (
    turn_id     uuid NOT NULL REFERENCES agent_turns (id) ON DELETE CASCADE,
    seq         bigint NOT NULL,
    event_type  text NOT NULL,
    body        jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (turn_id, seq)
);

CREATE INDEX agent_turn_events_tool_kept ON agent_turn_events (occurred_at)
WHERE event_type = 'tool.finished' AND body ->> 'output' <> '';

-- agent_approvals: a change an agent asked for, the request it would
-- make. Approving it makes the request as the approver.
CREATE TABLE agent_approvals (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES agent_conversations (id) ON DELETE CASCADE,
    turn_id         uuid NOT NULL REFERENCES agent_turns (id) ON DELETE CASCADE,
    member_id       uuid NOT NULL REFERENCES console_members (id),
    operation       text NOT NULL,
    method          text NOT NULL,
    path            text NOT NULL,
    headers         jsonb NOT NULL,
    body            jsonb,
    status          text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'failed')),
    decided_by      uuid REFERENCES console_members (id),
    decided_at      timestamptz,
    result_status   integer,
    result_body     jsonb,
    reported_at     timestamptz,
    created_at      timestamptz NOT NULL,
    CHECK ((status = 'pending') = (decided_at IS NULL))
);

CREATE INDEX agent_approvals_conversation ON agent_approvals (conversation_id, created_at DESC);
CREATE INDEX agent_approvals_turn ON agent_approvals (turn_id);

-- agent_turn_calls: every API call a turn's token made, kept as long as
-- a read of personal data is (90 days).
CREATE TABLE agent_turn_calls (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    turn_id   uuid NOT NULL REFERENCES agent_turns (id) ON DELETE CASCADE,
    operation text NOT NULL,
    method    text NOT NULL,
    path      text NOT NULL,
    status    integer NOT NULL,
    took_ms   integer NOT NULL,
    called_at timestamptz NOT NULL
);

CREATE INDEX agent_turn_calls_turn ON agent_turn_calls (turn_id);
CREATE INDEX agent_turn_calls_called_at ON agent_turn_calls (called_at);

-- The old questions and answers, as the history's user and assistant
-- messages.
INSERT INTO agent_messages (conversation_id, seq, turn_id, message, created_at)
SELECT
    conversation_id,
    row_number() OVER (PARTITION BY conversation_id ORDER BY ordinal, part)::integer,
    NULL,
    message,
    at
FROM (
    SELECT conversation_id, ordinal, 1 AS part, jsonb_build_object('role', 'user', 'content', question) AS message,
        started_at AS at
    FROM legacy_agent_turns
    UNION ALL
    SELECT conversation_id, ordinal, 2, jsonb_build_object('role', 'assistant', 'content', answer),
        COALESCE(finished_at, started_at)
    FROM legacy_agent_turns
    WHERE answer IS NOT NULL AND answer <> ''
) AS old;

-- A console agent holds capabilities instead of MCP tools: each old read
-- tool becomes the capability that reads the same records. count, lookup
-- and price_guide have none.
ALTER TABLE ai_agents RENAME COLUMN tools TO capabilities;
UPDATE ai_agents SET capabilities = ARRAY(
    SELECT DISTINCT c FROM unnest(capabilities) AS t (tool)
    CROSS JOIN LATERAL (VALUES (CASE
        WHEN tool IN ('ticket_get', 'ticket_messages', 'tickets_list', 'tickets_of_account') THEN 'tickets.read'
        WHEN tool IN ('account_get', 'accounts_list') THEN 'accounts.read'
        WHEN tool IN ('order_get', 'orders_of_account') THEN 'orders.read'
        WHEN tool IN ('listing_get', 'listings_of_account') THEN 'listings.read'
        WHEN tool IN ('report_cases_list', 'report_case_get') THEN 'reports.read'
        WHEN tool = 'console_actions' THEN 'audit.read'
        WHEN tool = 'feedback_get' THEN 'feedback.read'
    END)) AS m (c)
    WHERE c IS NOT NULL ORDER BY c
)
WHERE agent_kind = 'console';
-- The Console assistant also reads feedback, which no MCP tool served.
UPDATE ai_agents SET capabilities = ARRAY(SELECT DISTINCT c FROM unnest(capabilities || 'feedback.read'::text) AS t (c) ORDER BY c)
WHERE id = '0193e000-0000-7000-8000-00000000a101';

-- The Console assistant's seeded instructions spoke of the MCP tools; they
-- get a new version unless an admin changed them.
INSERT INTO ai_agent_versions (agent_id, version_no, instructions, created_by, created_at)
SELECT a.id, a.instructions_version + 1,
    'You help ShopNexus console members with tickets, accounts, orders, listings and reports. Look records up '
    || 'before you answer about them, and name the records you used.',
    NULL, now()
FROM ai_agents AS a
JOIN ai_agent_versions AS v ON v.agent_id = a.id AND v.version_no = a.instructions_version
WHERE a.id = '0193e000-0000-7000-8000-00000000a101'
    AND v.instructions = 'You help ShopNexus console members. Read records only through the tools, cite every record '
    || 'you use, and say so when the tools cannot answer.';
UPDATE ai_agents SET instructions_version = instructions_version + 1
WHERE id = '0193e000-0000-7000-8000-00000000a101' AND EXISTS (
    SELECT 1 FROM ai_agent_versions AS v
    WHERE v.agent_id = ai_agents.id AND v.version_no = ai_agents.instructions_version + 1
);
