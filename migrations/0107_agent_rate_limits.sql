-- agent_rate_limits: each time a member's message or steer was refused for
-- the rate limit, for the AI settings' guardrails. Kept as long as the
-- agent's API calls (90 days).
CREATE TABLE agent_rate_limits (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id  uuid NOT NULL,
    limited_at timestamptz NOT NULL
);

CREATE INDEX agent_rate_limits_limited_at ON agent_rate_limits (limited_at);
