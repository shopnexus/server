-- The ops dashboards (Grafana, Restate's UI, ...) open from the console on
-- hosts of their own. The console gets a one-time ticket, and the
-- dashboard's host turns it into a cookie of its own. Both rows hold the
-- SHA-256 of their token and belong to a console session; a dashboard session
-- lives only while that console session does, which every check joins.
CREATE TABLE console_dashboard_tickets (
    token_hash bytea PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES console_sessions ON DELETE CASCADE,
    app        text NOT NULL CHECK (app IN ('grafana', 'restate', 'litellm', 's3', 'argocd')),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);
CREATE INDEX console_dashboard_tickets_session ON console_dashboard_tickets (session_id);

-- expires_at copies the console session's end, which is also the cookie's.
CREATE TABLE console_dashboard_sessions (
    token_hash bytea PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES console_sessions ON DELETE CASCADE,
    app        text NOT NULL CHECK (app IN ('grafana', 'restate', 'litellm', 's3', 'argocd')),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX console_dashboard_sessions_session ON console_dashboard_sessions (session_id);
