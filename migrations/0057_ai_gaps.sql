-- Budget settings, member budgets and dashboard floors (2026-11-18).
--
-- ai_budget_settings is one row: the month's cap on all AI spend, the share
-- of it that raises the alert, what the cap does, and the daily budget of a
-- moderator and of an admin through the console agents. ai_member_budgets
-- holds a member's own daily budget in place of their role's.
CREATE TABLE ai_budget_settings (
    singleton           boolean PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    monthly_cap_usd     numeric(12, 2) CHECK (monthly_cap_usd >= 0),
    alert_pct           integer NOT NULL CHECK (alert_pct BETWEEN 1 AND 100),
    on_cap              text NOT NULL CHECK (on_cap IN ('stop_agents', 'alert_only')),
    moderator_daily_usd numeric(12, 2) CHECK (moderator_daily_usd >= 0),
    admin_daily_usd     numeric(12, 2) CHECK (admin_daily_usd >= 0),
    updated_by          uuid REFERENCES console_members (id),
    updated_at          timestamptz NOT NULL
);

INSERT INTO ai_budget_settings (singleton, monthly_cap_usd, alert_pct, on_cap, updated_at)
VALUES (TRUE, NULL, 80, 'alert_only', '2026-11-18T00:00:00Z');

CREATE TABLE ai_member_budgets (
    member_id  uuid PRIMARY KEY REFERENCES console_members (id),
    daily_usd  numeric(12, 2) NOT NULL CHECK (daily_usd >= 0),
    updated_by uuid REFERENCES console_members (id),
    updated_at timestamptz NOT NULL
);

-- The ops dashboards and the lowest role that opens each. The ticket and
-- the forwardAuth check read the floor here; an app with no row is
-- refused.
CREATE TABLE console_dashboards (
    app          text PRIMARY KEY,
    display_name text NOT NULL,
    floor_role   text NOT NULL CHECK (floor_role IN ('moderator', 'admin')),
    sort_order   integer NOT NULL,
    updated_by   uuid REFERENCES console_members (id),
    updated_at   timestamptz NOT NULL
);

INSERT INTO console_dashboards (app, display_name, floor_role, sort_order, updated_at) VALUES
('grafana', 'Grafana', 'moderator', 1, '2026-11-18T00:00:00Z'),
('restate', 'Restate', 'admin', 2, '2026-11-18T00:00:00Z'),
('litellm', 'LiteLLM', 'admin', 3, '2026-11-18T00:00:00Z'),
('s3', 'RustFS', 'admin', 4, '2026-11-18T00:00:00Z'),
('db', 'pgweb', 'admin', 5, '2026-11-18T00:00:00Z'),
('argocd', 'Argo CD', 'admin', 6, '2026-11-18T00:00:00Z');

ALTER TABLE console_actions DROP CONSTRAINT console_actions_target_kind_check;
ALTER TABLE console_actions ADD CONSTRAINT console_actions_target_kind_check
CHECK (
    target_kind IN (
        'account', 'member', 'ticket', 'listing', 'ai_provider', 'ai_model', 'ai_agent', 'ai_settings', 'dashboard'
    )
);
