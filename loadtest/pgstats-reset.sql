-- Before a run: start pg_stat_statements from zero, so the report after it
-- holds the run's statements only.
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
SELECT pg_stat_statements_reset();
