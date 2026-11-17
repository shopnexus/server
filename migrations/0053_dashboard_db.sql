-- pgweb opens from the console as the dashboard db.
ALTER TABLE console_dashboard_tickets
    DROP CONSTRAINT console_dashboard_tickets_app_check,
    ADD CONSTRAINT console_dashboard_tickets_app_check
    CHECK (app IN ('grafana', 'restate', 'litellm', 's3', 'argocd', 'db'));

ALTER TABLE console_dashboard_sessions
    DROP CONSTRAINT console_dashboard_sessions_app_check,
    ADD CONSTRAINT console_dashboard_sessions_app_check
    CHECK (app IN ('grafana', 'restate', 'litellm', 's3', 'argocd', 'db'));
