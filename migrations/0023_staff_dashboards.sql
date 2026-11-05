-- The ops dashboards (Grafana, Restate's UI, ...) open from the console on
-- hosts of their own. The console gets a one-time ticket, and the
-- dashboard's host turns it into a cookie of its own. Both rows hold the
-- SHA-256 of their token and belong to a staff session; a dashboard session
-- lives only while that staff session does, which every check joins.
CREATE TABLE staff_dashboard_tickets (
    token_hash       bytea PRIMARY KEY,
    staff_session_id uuid NOT NULL REFERENCES staff_sessions ON DELETE CASCADE,
    app              text NOT NULL CHECK (app IN ('grafana', 'restate', 'litellm', 's3', 'argocd')),
    created_at       timestamptz NOT NULL,
    expires_at       timestamptz NOT NULL,
    used_at          timestamptz
);
CREATE INDEX staff_dashboard_tickets_session ON staff_dashboard_tickets (staff_session_id);

-- expires_at copies the staff session's end, which is also the cookie's.
CREATE TABLE staff_dashboard_sessions (
    token_hash       bytea PRIMARY KEY,
    staff_session_id uuid NOT NULL REFERENCES staff_sessions ON DELETE CASCADE,
    app              text NOT NULL CHECK (app IN ('grafana', 'restate', 'litellm', 's3', 'argocd')),
    created_at       timestamptz NOT NULL,
    expires_at       timestamptz NOT NULL
);
CREATE INDEX staff_dashboard_sessions_session ON staff_dashboard_sessions (staff_session_id);
