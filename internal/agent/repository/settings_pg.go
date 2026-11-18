package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/audit"
)

type pgSettings struct{ db *pgxpool.Pool }

// NewSettings keeps the AI settings on db.
func NewSettings(db *pgxpool.Pool) Settings { return pgSettings{db} }

func (s pgSettings) Tags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.Query(ctx, `SELECT tag_name, tag_kind, sort_order FROM ai_tags ORDER BY sort_order`)
	if err != nil {
		return nil, fmt.Errorf("select ai tags: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Tag, error) {
		var t Tag
		if scanErr := r.Scan(&t.Name, &t.Kind, &t.Sort); scanErr != nil {
			return t, fmt.Errorf("scan ai tag: %w", scanErr)
		}
		return t, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read ai tags: %w", err)
	}
	return out, nil
}

func (s pgSettings) ModelTags(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.Query(ctx, `SELECT m.model_id, m.tag_name FROM ai_model_tags AS m
JOIN ai_tags AS t ON t.tag_name = m.tag_name ORDER BY m.model_id, t.sort_order`)
	if err != nil {
		return nil, fmt.Errorf("select model tags: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var model, tag string
		if err := rows.Scan(&model, &tag); err != nil {
			return nil, fmt.Errorf("read model tag: %w", err)
		}
		out[model] = append(out[model], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read model tags: %w", err)
	}
	return out, nil
}

func (s pgSettings) SetModelTags(ctx context.Context, model string, tags []string, e audit.Entry) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin model tags: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `DELETE FROM ai_model_tags WHERE model_id = @model`, pgx.StrictNamedArgs{"model": model}); err != nil {
		return fmt.Errorf("delete model tags: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ai_model_tags (model_id, tag_name) SELECT @model, unnest(@tags::text[])
ON CONFLICT DO NOTHING`, pgx.StrictNamedArgs{"model": model, "tags": tags}); err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23503" {
			return ErrUnknownTag
		}
		return fmt.Errorf("insert model tags: %w", err)
	}
	if err = audit.Insert(ctx, tx, e); err != nil {
		return fmt.Errorf("record model tags: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit model tags: %w", err)
	}
	return nil
}

const agentColumns = `id, agent_kind, display_name, model_alias, icon, purpose, required_tags, model_id, instructions,
    instructions_version, tools, limits, monthly_budget_usd::float8, key_aliases, enabled, updated_by, updated_at, write_id`

func scanAgent(r pgx.CollectableRow) (Agent, error) {
	var a Agent
	if err := r.Scan(&a.ID, &a.Kind, &a.Name, &a.Alias, &a.Icon, &a.Purpose, &a.RequiredTags, &a.Model, &a.Instructions,
		&a.InstructionsVersion, &a.Tools, &a.Limits, &a.MonthlyBudgetUSD, &a.KeyAliases, &a.Enabled, &a.UpdatedBy,
		&a.UpdatedAt, &a.WriteID); err != nil {
		return a, fmt.Errorf("scan ai agent: %w", err)
	}
	return a, nil
}

func (s pgSettings) Agents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.Query(ctx, `SELECT `+agentColumns+` FROM ai_agents ORDER BY agent_kind, display_name`)
	if err != nil {
		return nil, fmt.Errorf("select ai agents: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanAgent)
	if err != nil {
		return nil, fmt.Errorf("read ai agents: %w", err)
	}
	return out, nil
}

func (s pgSettings) Agent(ctx context.Context, id uuid.UUID) (Agent, error) {
	rows, err := s.db.Query(ctx, `SELECT `+agentColumns+` FROM ai_agents WHERE id = @id`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return Agent{}, fmt.Errorf("select ai agent: %w", err)
	}
	a, err := pgx.CollectExactlyOneRow(rows, scanAgent)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, fmt.Errorf("read ai agent: %w", err)
	}
	return a, nil
}

func agentArgs(a Agent) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"id": a.ID, "kind": a.Kind, "name": a.Name, "alias": a.Alias, "icon": a.Icon, "purpose": a.Purpose,
		"required": a.RequiredTags, "model": a.Model, "instructions": a.Instructions, "version": a.InstructionsVersion,
		"tools": a.Tools, "limits": a.Limits, "budget": a.MonthlyBudgetUSD, "keys": a.KeyAliases, "enabled": a.Enabled,
		"by": a.UpdatedBy, "at": a.UpdatedAt, "write_id": a.WriteID,
	}
}

// taken turns a unique violation of ai_agents into ErrTaken.
func taken(err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
		return ErrTaken
	}
	return err
}

func insertVersion(ctx context.Context, tx pgx.Tx, a Agent) error {
	_, err := tx.Exec(ctx, `INSERT INTO ai_agent_versions (agent_id, version_no, instructions, created_by, created_at)
VALUES (@id, @version, @instructions, @by, @at) ON CONFLICT DO NOTHING`, pgx.StrictNamedArgs{
		"id": a.ID, "version": a.InstructionsVersion, "instructions": a.Instructions, "by": a.UpdatedBy, "at": a.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert agent version: %w", err)
	}
	return nil
}

func (s pgSettings) CreateAgent(ctx context.Context, a Agent, e audit.Entry) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create agent: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO ai_agents (id, agent_kind, display_name, model_alias, icon, purpose, required_tags,
    model_id, instructions, instructions_version, tools, limits, monthly_budget_usd, key_aliases, enabled, updated_by,
    updated_at, write_id)
VALUES (@id, @kind, @name, @alias, @icon, @purpose, @required, @model, @instructions, @version, @tools, @limits, @budget,
    @keys, @enabled, @by, @at, @write_id)`, agentArgs(a)); err != nil {
		if errors.Is(taken(err), ErrTaken) {
			return ErrTaken
		}
		return fmt.Errorf("insert ai agent: %w", err)
	}
	if err = insertVersion(ctx, tx, a); err != nil {
		return err
	}
	if err = audit.Insert(ctx, tx, e); err != nil {
		return fmt.Errorf("record agent create: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create agent: %w", err)
	}
	return nil
}

func (s pgSettings) SaveAgent(ctx context.Context, a Agent, readWriteID uuid.UUID, e audit.Entry) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin save agent: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	args := agentArgs(a)
	delete(args, "kind")
	delete(args, "alias")
	delete(args, "keys")
	args["read_write_id"] = readWriteID
	tag, err := tx.Exec(ctx, `UPDATE ai_agents SET display_name = @name, icon = @icon, purpose = @purpose,
    required_tags = @required, model_id = @model, instructions = @instructions, instructions_version = @version,
    tools = @tools, limits = @limits, monthly_budget_usd = @budget, enabled = @enabled, updated_by = @by,
    updated_at = @at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`, args)
	if err != nil {
		if errors.Is(taken(err), ErrTaken) {
			return ErrTaken
		}
		return fmt.Errorf("update ai agent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	if err = insertVersion(ctx, tx, a); err != nil {
		return err
	}
	if err = audit.Insert(ctx, tx, e); err != nil {
		return fmt.Errorf("record agent update: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit save agent: %w", err)
	}
	return nil
}

func (s pgSettings) Versions(ctx context.Context, id uuid.UUID) ([]Version, error) {
	rows, err := s.db.Query(ctx, `SELECT version_no, instructions, created_by, created_at FROM ai_agent_versions
WHERE agent_id = @id ORDER BY version_no DESC`, pgx.StrictNamedArgs{"id": id})
	if err != nil {
		return nil, fmt.Errorf("select agent versions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Version, error) {
		var v Version
		if scanErr := r.Scan(&v.Version, &v.Instructions, &v.CreatedBy, &v.CreatedAt); scanErr != nil {
			return v, fmt.Errorf("scan agent version: %w", scanErr)
		}
		return v, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read agent versions: %w", err)
	}
	return out, nil
}

func (s pgSettings) Record(ctx context.Context, e audit.Entry) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = audit.Insert(ctx, tx, e); err != nil {
		return fmt.Errorf("record ai settings change: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit record: %w", err)
	}
	return nil
}

func (s pgSettings) BudgetSettings(ctx context.Context) (BudgetSettings, error) {
	var b BudgetSettings
	if err := s.db.QueryRow(ctx, `SELECT monthly_cap_usd::float8, alert_pct, on_cap, moderator_daily_usd::float8,
    admin_daily_usd::float8, updated_by, updated_at FROM ai_budget_settings`).Scan(&b.MonthlyCapUSD, &b.AlertPct, &b.OnCap,
		&b.ModeratorDailyUSD, &b.AdminDailyUSD, &b.UpdatedBy, &b.UpdatedAt); err != nil {
		return BudgetSettings{}, fmt.Errorf("select budget settings: %w", err)
	}
	rows, err := s.db.Query(ctx, `SELECT member_id, daily_usd::float8 FROM ai_member_budgets ORDER BY member_id`)
	if err != nil {
		return BudgetSettings{}, fmt.Errorf("select member budgets: %w", err)
	}
	b.Members, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (MemberBudget, error) {
		var m MemberBudget
		if scanErr := r.Scan(&m.MemberID, &m.DailyUSD); scanErr != nil {
			return m, fmt.Errorf("scan member budget: %w", scanErr)
		}
		return m, nil
	})
	if err != nil {
		return BudgetSettings{}, fmt.Errorf("read member budgets: %w", err)
	}
	return b, nil
}

func (s pgSettings) SaveBudgetSettings(ctx context.Context, b BudgetSettings, e audit.Entry) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin budget settings: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE ai_budget_settings SET monthly_cap_usd = @cap, alert_pct = @alert, on_cap = @on_cap,
    moderator_daily_usd = @moderator, admin_daily_usd = @admin, updated_by = @by, updated_at = @at`, pgx.StrictNamedArgs{
		"cap": b.MonthlyCapUSD, "alert": b.AlertPct, "on_cap": b.OnCap, "moderator": b.ModeratorDailyUSD,
		"admin": b.AdminDailyUSD, "by": b.UpdatedBy, "at": b.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("update budget settings: %w", err)
	}
	if b.SetMembers {
		if _, err = tx.Exec(ctx, `DELETE FROM ai_member_budgets`); err != nil {
			return fmt.Errorf("delete member budgets: %w", err)
		}
		for _, m := range b.Members {
			if _, err = tx.Exec(ctx, `INSERT INTO ai_member_budgets (member_id, daily_usd, updated_by, updated_at)
VALUES (@member, @usd, @by, @at)`, pgx.StrictNamedArgs{
				"member": m.MemberID, "usd": m.DailyUSD, "by": b.UpdatedBy, "at": b.UpdatedAt,
			}); err != nil {
				if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23503" {
					return ErrNotFound
				}
				return fmt.Errorf("insert member budget: %w", err)
			}
		}
	}
	if err = audit.Insert(ctx, tx, e); err != nil {
		return fmt.Errorf("record budget settings: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit budget settings: %w", err)
	}
	return nil
}
