-- Bifrost replaces LiteLLM (2026-11-26). The server owns the AI policy:
-- each agent's provider, model and fallbacks, its callers and their rate
-- limits, and the price of every model an agent uses. Its reconciler writes
-- them into Bifrost as teams, virtual keys and pricing overrides.

-- Checked at once, so no deferred check blocks the ALTERs after an UPDATE.
SET CONSTRAINTS ai_agents_latest_version IMMEDIATE;

-- An agent's slug is its stable name, its team's in Bifrost. Its model is
-- a provider's model, which callers send as provider/model.
ALTER TABLE ai_agents RENAME COLUMN model_alias TO slug;
ALTER TABLE ai_agents ADD COLUMN model_provider text;
UPDATE ai_agents SET model_provider = CASE WHEN strpos(model_id, '/') > 0 THEN 'openrouter' ELSE 'upstream' END;
UPDATE ai_agents SET model_provider = 'openrouter', model_id = 'google/gemini-2.5-flash'
WHERE id = '0193e000-0000-7000-8000-00000000a104';
UPDATE ai_agents SET model_provider = 'openrouter', model_id = 'baai/bge-m3'
WHERE id = '0193e000-0000-7000-8000-00000000a105';
ALTER TABLE ai_agents ALTER COLUMN model_provider SET NOT NULL;

-- fallbacks are provider/model, tried in order. extra_body goes into each
-- request to the model, such as OpenRouter's provider pin.
ALTER TABLE ai_agents ADD COLUMN fallbacks text[] NOT NULL DEFAULT '{}';
ALTER TABLE ai_agents ADD COLUMN extra_body jsonb NOT NULL DEFAULT '{}';
UPDATE ai_agents SET extra_body = '{"provider": {"order": ["DeepInfra"], "allow_fallbacks": false}}'
WHERE id = '0193e000-0000-7000-8000-00000000a105';

-- A budget is in dollars now, monthly; Bifrost takes none of 0.
UPDATE ai_agents SET monthly_budget_usd = 100 WHERE id = '0193e000-0000-7000-8000-00000000a101';
UPDATE ai_agents SET monthly_budget_usd = 30 WHERE id IN (
    '0193e000-0000-7000-8000-00000000a102', '0193e000-0000-7000-8000-00000000a103',
    '0193e000-0000-7000-8000-00000000a104', '0193e000-0000-7000-8000-00000000a105'
);
UPDATE ai_agents SET monthly_budget_usd = NULL WHERE monthly_budget_usd = 0;
ALTER TABLE ai_agents DROP CONSTRAINT ai_agents_monthly_budget_usd_check;
ALTER TABLE ai_agents ADD CONSTRAINT ai_agents_monthly_budget_usd_check CHECK (monthly_budget_usd > 0);

-- A caller is one virtual key of an agent, named once across agents, with
-- its rate limits per minute. A console agent's one caller is its slug.
CREATE TABLE ai_agent_callers (
    caller_name      text PRIMARY KEY CHECK (caller_name ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    agent_id         uuid NOT NULL REFERENCES ai_agents (id),
    requests_per_min integer CHECK (requests_per_min > 0),
    tokens_per_min   integer CHECK (tokens_per_min > 0)
);
CREATE INDEX ai_agent_callers_agent ON ai_agent_callers (agent_id);

INSERT INTO ai_agent_callers (caller_name, agent_id, requests_per_min, tokens_per_min) VALUES
('console-agent', '0193e000-0000-7000-8000-00000000a101', NULL, 200000),
('server-trust', '0193e000-0000-7000-8000-00000000a102', NULL, 150000),
('server-assist', '0193e000-0000-7000-8000-00000000a103', NULL, 300000),
('server-search', '0193e000-0000-7000-8000-00000000a104', NULL, 100000),
('server-embed-index', '0193e000-0000-7000-8000-00000000a105', 600, NULL),
('server-embed-search', '0193e000-0000-7000-8000-00000000a105', 600, NULL);

INSERT INTO ai_agent_callers (caller_name, agent_id)
SELECT slug, id FROM ai_agents
WHERE agent_kind = 'console' AND id <> '0193e000-0000-7000-8000-00000000a101'
ON CONFLICT DO NOTHING;

ALTER TABLE ai_agents DROP COLUMN key_aliases;

-- The price of each model an agent uses, in USD per token, which Bifrost
-- logs every call's cost with. A null cache price is the input price.
CREATE TABLE ai_model_prices (
    provider                  text NOT NULL,
    model_id                  text NOT NULL,
    input_usd_per_token       numeric NOT NULL CHECK (input_usd_per_token >= 0),
    output_usd_per_token      numeric NOT NULL CHECK (output_usd_per_token >= 0),
    cache_read_usd_per_token  numeric CHECK (cache_read_usd_per_token >= 0),
    cache_write_usd_per_token numeric CHECK (cache_write_usd_per_token >= 0),
    updated_by                uuid REFERENCES console_members (id),
    updated_at                timestamptz NOT NULL,
    PRIMARY KEY (provider, model_id)
);

-- List prices of Bifrost's catalog on 2026-11-26 (Anthropic's for the
-- Claude models).
INSERT INTO ai_model_prices (
    provider, model_id, input_usd_per_token, output_usd_per_token, cache_read_usd_per_token,
    cache_write_usd_per_token, updated_at
) VALUES
('upstream', 'claude-sonnet-5-5', 0.000002, 0.00001, 0.0000001, 0.0000025, '2026-11-26T00:00:00Z'),
('upstream', 'claude-haiku-5-5', 0.0000001, 0.0000005, 0.00000001, 0.000000125, '2026-11-26T00:00:00Z'),
('openrouter', 'google/gemini-2.5-flash', 0.0000003, 0.0000025, 0.00000003, 0.0000000833, '2026-11-26T00:00:00Z'),
('openrouter', 'baai/bge-m3', 0.00000001, 0, NULL, NULL, '2026-11-26T00:00:00Z');

-- Tags belong to a provider's model.
ALTER TABLE ai_model_tags ADD COLUMN provider text;
UPDATE ai_model_tags SET provider = CASE WHEN strpos(model_id, '/') > 0 THEN 'openrouter' ELSE 'upstream' END;
ALTER TABLE ai_model_tags ALTER COLUMN provider SET NOT NULL;
ALTER TABLE ai_model_tags DROP CONSTRAINT ai_model_tags_pkey;
ALTER TABLE ai_model_tags ADD PRIMARY KEY (provider, model_id, tag_name);

INSERT INTO ai_model_tags (provider, model_id, tag_name) VALUES
('openrouter', 'google/gemini-2.5-flash', 'Chat'),
('openrouter', 'google/gemini-2.5-flash', 'Vision'),
('openrouter', 'google/gemini-2.5-flash', 'Tool use'),
('openrouter', 'google/gemini-2.5-flash', 'Long context'),
('openrouter', 'google/gemini-2.5-flash', 'Reasoning'),
('openrouter', 'google/gemini-2.5-flash', 'Structured output'),
('openrouter', 'google/gemini-2.5-flash', 'Multilingual')
ON CONFLICT DO NOTHING;

-- One monthly budget per agent replaces the cap and the members' daily
-- budgets.
DROP TABLE ai_member_budgets;
DROP TABLE ai_budget_settings;

-- Bifrost's UI replaces LiteLLM's as a dashboard.
DELETE FROM console_dashboard_tickets WHERE app = 'litellm';
DELETE FROM console_dashboard_sessions WHERE app = 'litellm';
ALTER TABLE console_dashboard_tickets
    DROP CONSTRAINT console_dashboard_tickets_app_check,
    ADD CONSTRAINT console_dashboard_tickets_app_check
    CHECK (app IN ('grafana', 'restate', 'bifrost', 's3', 'argocd', 'db'));
ALTER TABLE console_dashboard_sessions
    DROP CONSTRAINT console_dashboard_sessions_app_check,
    ADD CONSTRAINT console_dashboard_sessions_app_check
    CHECK (app IN ('grafana', 'restate', 'bifrost', 's3', 'argocd', 'db'));
UPDATE console_dashboards SET app = 'bifrost', display_name = 'Bifrost' WHERE app = 'litellm';
