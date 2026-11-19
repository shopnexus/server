-- The console agent's runs (2026-11-19): the server keeps the
-- conversations, turns, events, tool calls, documents and skills, and the
-- agent service only runs a turn's segments and streams them back.
--
-- agent_conversations: a conversation with a subject is shared by every
-- member, one per subject; one without a subject is its member's own.
CREATE TABLE agent_conversations (
    id               uuid PRIMARY KEY,
    member_id        uuid NOT NULL REFERENCES console_members (id),
    subject_kind     text CHECK (subject_kind IN ('ticket', 'report_case', 'account')),
    subject_id       uuid,
    title            text,
    status           text NOT NULL CHECK (status IN ('active', 'archived', 'closed')),
    created_at       timestamptz NOT NULL,
    updated_at       timestamptz NOT NULL,
    last_activity_at timestamptz NOT NULL,
    CHECK ((subject_kind IS NULL) = (subject_id IS NULL))
);

CREATE UNIQUE INDEX agent_conversations_subject ON agent_conversations (subject_kind, subject_id)
WHERE subject_kind IS NOT NULL;
CREATE INDEX agent_conversations_own ON agent_conversations (member_id, last_activity_at DESC)
WHERE subject_kind IS NULL;

-- agent_turns: a question and its run. state is the agent's checkpoint,
-- which the server reads only for pending_ask and sources. A conversation
-- has at most one unfinished turn.
CREATE TABLE agent_turns (
    id                 uuid PRIMARY KEY,
    conversation_id    uuid NOT NULL REFERENCES agent_conversations (id) ON DELETE CASCADE,
    member_id          uuid NOT NULL REFERENCES console_members (id),
    ordinal            integer NOT NULL,
    status             text NOT NULL CHECK (
        status IN ('running', 'paused', 'awaiting_input', 'succeeded', 'failed', 'cancelled', 'timed_out')
    ),
    question           text NOT NULL,
    answer             text,
    blocks             jsonb,
    sources            jsonb,
    agent_id           text,
    agent_name         text,
    model_alias        text,
    error_code         text,
    input_tokens       bigint NOT NULL DEFAULT 0,
    output_tokens      bigint NOT NULL DEFAULT 0,
    cache_read_tokens  bigint NOT NULL DEFAULT 0,
    cache_write_tokens bigint NOT NULL DEFAULT 0,
    cost_usd           numeric(12, 6) NOT NULL DEFAULT 0,
    first_token_ms     integer,
    provider_name      text,
    started_at         timestamptz NOT NULL,
    run_started_at     timestamptz NOT NULL,
    deadline_ms        integer NOT NULL,
    finished_at        timestamptz,
    latency_ms         bigint,
    event_seq          bigint NOT NULL DEFAULT 0,
    wrote              boolean NOT NULL DEFAULT FALSE,
    state              jsonb,
    UNIQUE (conversation_id, ordinal)
);

CREATE UNIQUE INDEX agent_turns_unfinished ON agent_turns (conversation_id)
WHERE status IN ('running', 'paused', 'awaiting_input');
CREATE INDEX agent_turns_started_at ON agent_turns (started_at);
CREATE INDEX agent_turns_running ON agent_turns (run_started_at) WHERE status = 'running';

-- agent_turn_inputs: what members sent a turn. Pause, stop and the steers
-- of a running turn are the queue the agent polls by seq; steers and
-- answers also show in the turn and its history.
CREATE TABLE agent_turn_inputs (
    seq        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id         uuid NOT NULL UNIQUE,
    turn_id    uuid NOT NULL REFERENCES agent_turns (id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('pause', 'stop', 'steer', 'answers')),
    input_text text,
    answers    jsonb,
    read_at    timestamptz,
    created_at timestamptz NOT NULL
);

CREATE INDEX agent_turn_inputs_turn ON agent_turn_inputs (turn_id, seq);

-- agent_tool_calls: one row per tool call of a run, its id chosen by the
-- agent. content is the bounded result the history replays.
CREATE TABLE agent_tool_calls (
    id             uuid PRIMARY KEY,
    turn_id        uuid NOT NULL REFERENCES agent_turns (id) ON DELETE CASCADE,
    tool_use_id    text NOT NULL,
    tool_name      text NOT NULL,
    tool_label     text,
    tool_source    text,
    tool_input     jsonb NOT NULL,
    tool_output    text,
    result_content jsonb,
    output_spilled boolean NOT NULL DEFAULT FALSE,
    status         text NOT NULL CHECK (status IN ('pending', 'running', 'success', 'failed', 'cancelled')),
    latency_ms     integer,
    attempts       integer NOT NULL DEFAULT 0,
    attempt_errors jsonb,
    tool_timing    jsonb,
    created_at     timestamptz NOT NULL,
    finished_at    timestamptz,
    UNIQUE (turn_id, tool_use_id)
);

CREATE INDEX agent_tool_calls_created_at ON agent_tool_calls (created_at);
CREATE INDEX agent_tool_calls_tool ON agent_tool_calls (tool_name, created_at DESC);

-- agent_turn_events: a turn's events; seq counts from 1 per turn and comes
-- from agent_turns.event_seq.
CREATE TABLE agent_turn_events (
    turn_id     uuid NOT NULL REFERENCES agent_turns (id) ON DELETE CASCADE,
    seq         bigint NOT NULL,
    event_type  text NOT NULL,
    body        jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (turn_id, seq)
);

CREATE INDEX agent_turn_events_type ON agent_turn_events (event_type, occurred_at);

-- agent_documents: what write_document wrote, each write a new version.
CREATE TABLE agent_documents (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES agent_conversations (id) ON DELETE CASCADE,
    turn_id         uuid REFERENCES agent_turns (id) ON DELETE SET NULL,
    title           text NOT NULL,
    latest_version  integer NOT NULL,
    created_by      uuid REFERENCES console_members (id),
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

CREATE TABLE agent_document_versions (
    document_id uuid NOT NULL REFERENCES agent_documents (id) ON DELETE CASCADE,
    version_no  integer NOT NULL,
    title       text NOT NULL,
    sections    jsonb NOT NULL,
    turn_id     uuid REFERENCES agent_turns (id) ON DELETE SET NULL,
    created_by  uuid REFERENCES console_members (id),
    created_at  timestamptz NOT NULL,
    PRIMARY KEY (document_id, version_no)
);

-- agent_skills: saved questions under / in the ask box, shared by every
-- member.
CREATE TABLE agent_skills (
    id              uuid PRIMARY KEY,
    created_by      uuid NOT NULL REFERENCES console_members (id),
    skill_name      text NOT NULL,
    slug            text NOT NULL UNIQUE,
    description     text NOT NULL,
    prompt          text NOT NULL,
    skill_variables jsonb NOT NULL,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);
