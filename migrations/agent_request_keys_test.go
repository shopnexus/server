package migrations_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// 0094 gives a skill from before a write ID and the budget settings one,
// and lets a member's request key claim one turn.
func TestAgentRequestKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0094_agent_request_keys.sql")); err != nil {
		t.Fatal(err)
	}
	member, skill, conv := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	at := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	for _, q := range []struct {
		sql  string
		args pgx.StrictNamedArgs
	}{
		{
			`INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
    credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@member, 'lan@console.test', 'Lan', 'moderator', 'active', 'x', false, 1, 0, @at, @at, @member)`,
			pgx.StrictNamedArgs{"member": member, "at": at},
		},
		{`INSERT INTO agent_skills (id, created_by, skill_name, slug, description, prompt, skill_variables, created_at, updated_at)
VALUES (@skill, @member, 'a', 'a', '', '?', '[]', @at, @at)`, pgx.StrictNamedArgs{"skill": skill, "member": member, "at": at}},
		{`INSERT INTO agent_conversations (id, member_id, status, created_at, updated_at, last_activity_at)
VALUES (@conv, @member, 'active', @at, @at, @at)`, pgx.StrictNamedArgs{"conv": conv, "member": member, "at": at}},
	} {
		if _, err := db.Exec(ctx, q.sql, q.args); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	// 0103 drops the budget settings.
	if _, err := postgres.Migrate(ctx, db, before(t, "0103_bifrost.sql")); err != nil {
		t.Fatal(err)
	}
	var skillWrite, budgetWrite uuid.UUID
	err := db.QueryRow(ctx, `SELECT write_id FROM agent_skills WHERE id = @skill`, pgx.StrictNamedArgs{"skill": skill}).
		Scan(&skillWrite)
	if err != nil || skillWrite != skill {
		t.Errorf("the skill's write ID %s %v", skillWrite, err)
	}
	if err = db.QueryRow(ctx, `SELECT write_id FROM ai_budget_settings`).Scan(&budgetWrite); err != nil || budgetWrite == uuid.Nil() {
		t.Errorf("the budget settings' write ID %s %v", budgetWrite, err)
	}
	turn := `INSERT INTO agent_turns (id, conversation_id, member_id, ordinal, status, question, started_at, run_started_at,
    deadline_ms, request_key)
VALUES (uuidv7(), @conv, @member, @ordinal, 'succeeded', '?', @at, @at, 60000, @key)`
	a := pgx.StrictNamedArgs{"conv": conv, "member": member, "at": at, "key": uuid.NewV7()}
	for ordinal, wantErr := range []bool{false, true} {
		a["ordinal"] = ordinal
		if _, err = db.Exec(ctx, turn, a); (err != nil) != wantErr {
			t.Errorf("turn %d with the key: %v", ordinal, err)
		}
	}
	a["key"] = nil
	for ordinal := 2; ordinal < 4; ordinal++ {
		a["ordinal"] = ordinal
		if _, err = db.Exec(ctx, turn, a); err != nil {
			t.Errorf("turn %d without a key: %v", ordinal, err)
		}
	}
}
