package migrations_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/migrations"
)

// 0098 gives each agent's latest version the instructions the agent ran
// with, adding a missing one, and then needs that version for every agent.
func TestAgentCleanup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0098_agent_cleanup.sql")); err != nil {
		t.Fatal(err)
	}
	assistant := uuid.MustParse("0193e000-0000-7000-8000-00000000a101")
	stray := uuid.NewV7()
	for _, q := range []struct {
		sql  string
		args pgx.StrictNamedArgs
	}{
		{`UPDATE ai_agents SET instructions = 'Be brief.' WHERE id = @id`, pgx.StrictNamedArgs{"id": assistant}},
		{`INSERT INTO ai_agents (id, agent_kind, display_name, model_alias, icon, purpose, required_tags, model_id, instructions,
    instructions_version, tools, limits, key_aliases, enabled, updated_at, write_id)
VALUES (@id, 'console', 'Stray', 'stray-agent', 'x', '', '{}', 'claude-haiku-5-5', 'Stray.', 2, '{}', '{}', '{agent}', TRUE,
    now(), @id)`, pgx.StrictNamedArgs{"id": stray}},
	} {
		if _, err := db.Exec(ctx, q.sql, q.args); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[uuid.UUID]string{assistant: "Be brief.", stray: "Stray."} {
		var got string
		err := db.QueryRow(ctx, `SELECT v.instructions FROM ai_agents AS a
JOIN ai_agent_versions AS v ON v.agent_id = a.id AND v.version_no = a.instructions_version WHERE a.id = @id`,
			pgx.StrictNamedArgs{"id": id}).Scan(&got)
		if err != nil || got != want {
			t.Errorf("agent %s's instructions %q %v, want %q", id, got, err, want)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE ai_agents SET instructions_version = 9 WHERE id = @id`,
		pgx.StrictNamedArgs{"id": assistant}); err == nil {
		t.Error("an agent took a version that does not exist")
	}
}
