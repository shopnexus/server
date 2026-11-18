-- AI settings (2026-11-18): what LiteLLM has no place for. Providers,
-- deployments, router settings and budgets live in LiteLLM's database; the
-- server keeps the model tags and the agents.
--
-- ai_tags are the tags an admin puts on a model: its type (Chat, Embedding)
-- and what it can do. ai_model_tags names a model as the stack calls it
-- without LiteLLM's provider prefix (claude-haiku-5-5, baai/bge-m3).
CREATE TABLE ai_tags (
    tag_name   text PRIMARY KEY,
    tag_kind   text NOT NULL CHECK (tag_kind IN ('type', 'capability')),
    sort_order integer NOT NULL
);

CREATE TABLE ai_model_tags (
    model_id text NOT NULL,
    tag_name text NOT NULL REFERENCES ai_tags (tag_name),
    PRIMARY KEY (model_id, tag_name)
);

INSERT INTO ai_tags (tag_name, tag_kind, sort_order) VALUES
('Chat', 'type', 1),
('Embedding', 'type', 2),
('Vision', 'capability', 3),
('Tool use', 'capability', 4),
('Long context', 'capability', 5),
('Reasoning', 'capability', 6),
('Structured output', 'capability', 7),
('Multilingual', 'capability', 8);

INSERT INTO ai_model_tags (model_id, tag_name) VALUES
('claude-sonnet-5-5', 'Chat'),
('claude-sonnet-5-5', 'Vision'),
('claude-sonnet-5-5', 'Tool use'),
('claude-sonnet-5-5', 'Long context'),
('claude-sonnet-5-5', 'Reasoning'),
('claude-sonnet-5-5', 'Structured output'),
('claude-sonnet-5-5', 'Multilingual'),
('claude-haiku-5-5', 'Chat'),
('claude-haiku-5-5', 'Vision'),
('claude-haiku-5-5', 'Tool use'),
('claude-haiku-5-5', 'Long context'),
('claude-haiku-5-5', 'Structured output'),
('claude-haiku-5-5', 'Multilingual'),
('baai/bge-m3', 'Embedding'),
('baai/bge-m3', 'Multilingual');

-- An agent is one purpose with a readable name and an alias, the
-- model_name its callers send LiteLLM. App agents are called by the
-- server's code: an admin changes their model, limits and budget only.
-- Console agents run in the agent service with the instructions, tools
-- and limits here, which the server sends with each turn. key_aliases are
-- the LiteLLM keys that call the alias, whose budget is the agent's.
CREATE TABLE ai_agents (
    id                   uuid PRIMARY KEY,
    agent_kind           text NOT NULL CHECK (agent_kind IN ('app', 'console')),
    display_name         text NOT NULL UNIQUE CHECK (display_name <> ''),
    model_alias          text NOT NULL UNIQUE CHECK (model_alias ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    icon                 text NOT NULL,
    purpose              text NOT NULL,
    required_tags        text[] NOT NULL,
    model_id             text NOT NULL,
    instructions         text NOT NULL,
    instructions_version integer NOT NULL CHECK (instructions_version >= 1),
    tools                text[] NOT NULL,
    limits               jsonb NOT NULL,
    monthly_budget_usd   numeric(12, 2) CHECK (monthly_budget_usd >= 0),
    key_aliases          text[] NOT NULL,
    enabled              boolean NOT NULL,
    updated_by           uuid REFERENCES console_members (id),
    updated_at           timestamptz NOT NULL,
    write_id             uuid NOT NULL,
    CHECK (agent_kind = 'console' OR (instructions = '' AND cardinality(tools) = 0))
);

-- Each version of a console agent's instructions, who wrote it and when.
CREATE TABLE ai_agent_versions (
    agent_id     uuid NOT NULL REFERENCES ai_agents (id),
    version_no   integer NOT NULL,
    instructions text NOT NULL,
    created_by   uuid REFERENCES console_members (id),
    created_at   timestamptz NOT NULL,
    PRIMARY KEY (agent_id, version_no)
);

INSERT INTO ai_agents (
    id, agent_kind, display_name, model_alias, icon, purpose, required_tags, model_id, instructions,
    instructions_version, tools, limits, monthly_budget_usd, key_aliases, enabled, updated_by,
    updated_at, write_id
) VALUES
(
    '0193e000-0000-7000-8000-00000000a101', 'console', 'Console assistant', 'console-agent', 'sparkles',
    'Answers members'' questions about tickets, accounts, orders, listings and reports.',
    '{Chat,"Tool use"}', 'claude-sonnet-5-5',
    'You help ShopNexus console members. Read records only through the tools, cite every record you '
    || 'use, and say so when the tools cannot answer.',
    1,
    ARRAY[
        'count', 'lookup', 'ticket_get', 'ticket_messages', 'tickets_list', 'tickets_of_account', 'account_get',
        'accounts_list', 'orders_of_account', 'listings_of_account', 'console_actions', 'order_get', 'listing_get',
        'price_guide', 'report_cases_list', 'report_case_get'
    ],
    '{"deadline_s": 60, "max_model_calls": 40, "max_tokens": 4096}', NULL, '{agent}', TRUE, NULL,
    '2026-11-18T00:00:00Z', '0193e000-0000-7000-8000-00000000a101'
),
(
    '0193e000-0000-7000-8000-00000000a102', 'app', 'Scam check', 'scam-check', 'shield',
    'Reads a chat message and says whether it looks like a scam.',
    '{Chat,"Structured output",Multilingual}', 'claude-haiku-5-5', '', 1, '{}',
    '{"timeout_s": 20}', NULL, '{server-trust}', TRUE, NULL,
    '2026-11-18T00:00:00Z', '0193e000-0000-7000-8000-00000000a102'
),
(
    '0193e000-0000-7000-8000-00000000a103', 'app', 'Listing photo reader', 'listing-read', 'camera',
    'Reads a seller''s photos, video or link and fills in the listing.',
    '{Chat,Vision,Multilingual}', 'claude-haiku-5-5', '', 1, '{}',
    '{"timeout_s": 30}', NULL, '{server-assist}', TRUE, NULL,
    '2026-11-18T00:00:00Z', '0193e000-0000-7000-8000-00000000a103'
),
(
    '0193e000-0000-7000-8000-00000000a104', 'app', 'Search', 'search', 'search',
    'Turns what a buyer types into a search.',
    '{Chat,"Structured output",Multilingual}', 'claude-haiku-5-5', '', 1, '{}',
    '{"timeout_s": 15}', NULL, '{server-llm}', TRUE, NULL,
    '2026-11-18T00:00:00Z', '0193e000-0000-7000-8000-00000000a104'
),
(
    '0193e000-0000-7000-8000-00000000a105', 'app', 'Search embeddings', 'embed-text', 'vector',
    'Turns listing text and search queries into vectors.',
    '{Embedding,Multilingual}', 'baai/bge-m3', '', 1, '{}',
    '{"timeout_s": 15}', NULL, '{server-embed-index,server-embed-search}', TRUE, NULL,
    '2026-11-18T00:00:00Z', '0193e000-0000-7000-8000-00000000a105'
);

INSERT INTO ai_agent_versions (agent_id, version_no, instructions, created_by, created_at)
SELECT
    id,
    instructions_version,
    instructions,
    NULL AS created_by,
    updated_at
FROM ai_agents;

-- Changes to AI settings are console actions on a provider, a model, an
-- agent or the settings as a whole.
ALTER TABLE console_actions DROP CONSTRAINT console_actions_target_kind_check;
ALTER TABLE console_actions ADD CONSTRAINT console_actions_target_kind_check
CHECK (target_kind IN ('account', 'member', 'ticket', 'listing', 'ai_provider', 'ai_model', 'ai_agent', 'ai_settings'));
