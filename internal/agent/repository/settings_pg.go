package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/postgres"
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

func (s pgSettings) ModelTags(ctx context.Context) (map[domain.ModelRef][]string, error) {
	rows, err := s.db.Query(ctx, `SELECT m.provider, m.model_id, m.tag_name FROM ai_model_tags AS m
JOIN ai_tags AS t ON t.tag_name = m.tag_name ORDER BY m.provider, m.model_id, t.sort_order`)
	if err != nil {
		return nil, fmt.Errorf("select model tags: %w", err)
	}
	defer rows.Close()
	out := map[domain.ModelRef][]string{}
	for rows.Next() {
		var m domain.ModelRef
		var tag string
		if err := rows.Scan(&m.Provider, &m.Model, &tag); err != nil {
			return nil, fmt.Errorf("read model tag: %w", err)
		}
		out[m] = append(out[m], tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read model tags: %w", err)
	}
	return out, nil
}

func (s pgSettings) SetModelTags(ctx context.Context, model domain.ModelRef, tags []string, e audit.Entry) error {
	b := &pgx.Batch{}
	args := pgx.StrictNamedArgs{"provider": model.Provider, "model": model.Model}
	// Two sets of one model take turns, else each delete misses the other's
	// insert and the model ends with both lists.
	postgres.QueueLock(b, "model_tags", model.String())
	b.Queue(`DELETE FROM ai_model_tags WHERE provider = @provider AND model_id = @model`, args).Fn = postgres.ExecAs("delete model tags", nil)
	b.Queue(`INSERT INTO ai_model_tags (provider, model_id, tag_name) SELECT @provider, @model, unnest(@tags::text[])
ON CONFLICT DO NOTHING`, pgx.StrictNamedArgs{"provider": model.Provider, "model": model.Model, "tags": tags}).Fn = func(br pgx.BatchResults) error {
		_, err := br.Exec()
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23503" {
			return ErrUnknownTag
		}
		if err != nil {
			return fmt.Errorf("insert model tags: %w", err)
		}
		return nil
	}
	audit.Queue(b, e)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("set model tags: %w", err)
	}
	return nil
}

func (s pgSettings) Prices(ctx context.Context) ([]Price, error) {
	rows, err := s.db.Query(ctx, `SELECT provider, model_id, input_usd_per_token::float8, output_usd_per_token::float8,
    cache_read_usd_per_token::float8, cache_write_usd_per_token::float8, updated_by, updated_at
FROM ai_model_prices ORDER BY provider, model_id`)
	if err != nil {
		return nil, fmt.Errorf("select model prices: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Price, error) {
		var p Price
		if scanErr := r.Scan(&p.Model.Provider, &p.Model.Model, &p.Input, &p.Output, &p.CacheRead, &p.CacheWrite,
			&p.UpdatedBy, &p.UpdatedAt); scanErr != nil {
			return p, fmt.Errorf("scan model price: %w", scanErr)
		}
		return p, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read model prices: %w", err)
	}
	return out, nil
}

func (s pgSettings) SetPrice(ctx context.Context, p Price, e audit.Entry) error {
	b := &pgx.Batch{}
	b.Queue(`INSERT INTO ai_model_prices (provider, model_id, input_usd_per_token, output_usd_per_token,
    cache_read_usd_per_token, cache_write_usd_per_token, updated_by, updated_at)
VALUES (@provider, @model, @input, @output, @cache_read, @cache_write, @by, @at)
ON CONFLICT (provider, model_id) DO UPDATE SET input_usd_per_token = EXCLUDED.input_usd_per_token,
    output_usd_per_token = EXCLUDED.output_usd_per_token, cache_read_usd_per_token = EXCLUDED.cache_read_usd_per_token,
    cache_write_usd_per_token = EXCLUDED.cache_write_usd_per_token, updated_by = EXCLUDED.updated_by,
    updated_at = EXCLUDED.updated_at`, pgx.StrictNamedArgs{
		"provider": p.Model.Provider, "model": p.Model.Model, "input": p.Input, "output": p.Output,
		"cache_read": p.CacheRead, "cache_write": p.CacheWrite, "by": p.UpdatedBy, "at": p.UpdatedAt,
	}).Fn = postgres.ExecAs("upsert model price", nil)
	audit.Queue(b, e)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("set model price: %w", err)
	}
	return nil
}

// selectAgents reads the agents with their latest version's instructions
// and their callers.
const selectAgents = `SELECT a.id, a.agent_kind, a.display_name, a.slug, a.icon, a.purpose, a.required_tags,
    a.model_provider, a.model_id, a.fallbacks, a.extra_body, v.instructions, a.instructions_version, a.capabilities, a.limits,
    a.monthly_budget_usd::float8, a.enabled, a.updated_by, a.updated_at, a.write_id,
    COALESCE((SELECT jsonb_agg(jsonb_build_object('name', c.caller_name, 'requests_per_min', c.requests_per_min,
        'tokens_per_min', c.tokens_per_min) ORDER BY c.caller_name)
        FROM ai_agent_callers AS c WHERE c.agent_id = a.id), '[]')
FROM ai_agents AS a JOIN ai_agent_versions AS v ON v.agent_id = a.id AND v.version_no = a.instructions_version`

func scanAgent(r pgx.CollectableRow) (Agent, error) {
	var a Agent
	var fallbacks []string
	if err := r.Scan(&a.ID, &a.Kind, &a.Name, &a.Slug, &a.Icon, &a.Purpose, &a.RequiredTags, &a.Model.Provider, &a.Model.Model,
		&fallbacks, &a.ExtraBody, &a.Instructions, &a.InstructionsVersion, &a.Capabilities, &a.Limits, &a.MonthlyBudgetUSD, &a.Enabled,
		&a.UpdatedBy, &a.UpdatedAt, &a.WriteID, &a.Callers); err != nil {
		return a, fmt.Errorf("scan ai agent: %w", err)
	}
	a.Fallbacks = make([]domain.ModelRef, 0, len(fallbacks))
	for _, f := range fallbacks {
		m, ok := domain.ParseModelRef(f)
		if !ok {
			return a, fmt.Errorf("ai agent %s has fallback %q, not provider/model", a.Slug, f)
		}
		a.Fallbacks = append(a.Fallbacks, m)
	}
	return a, nil
}

func (s pgSettings) Agents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.Query(ctx, selectAgents+` ORDER BY a.agent_kind, a.display_name`)
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
	rows, err := s.db.Query(ctx, selectAgents+` WHERE a.id = @id`, pgx.StrictNamedArgs{"id": id})
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
	fallbacks := make([]string, 0, len(a.Fallbacks))
	for _, f := range a.Fallbacks {
		fallbacks = append(fallbacks, f.String())
	}
	extra := a.ExtraBody
	if extra == nil {
		extra = map[string]any{}
	}
	return pgx.StrictNamedArgs{
		"id": a.ID, "kind": a.Kind, "name": a.Name, "slug": a.Slug, "icon": a.Icon, "purpose": a.Purpose,
		"required": a.RequiredTags, "provider": a.Model.Provider, "model": a.Model.Model, "fallbacks": fallbacks,
		"extra": extra, "version": a.InstructionsVersion, "capabilities": a.Capabilities, "limits": a.Limits,
		"budget": a.MonthlyBudgetUSD, "enabled": a.Enabled, "by": a.UpdatedBy, "at": a.UpdatedAt, "write_id": a.WriteID,
	}
}

// taken turns a unique violation into ErrTaken.
func taken(what string) func(pgx.BatchResults) error {
	return func(br pgx.BatchResults) error {
		_, err := br.Exec()
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrTaken
		}
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		return nil
	}
}

// queueVersion queues the insert of a's instructions as its version; a
// version that exists fails the batch.
func queueVersion(b *pgx.Batch, a Agent) {
	b.Queue(`INSERT INTO ai_agent_versions (agent_id, version_no, instructions, created_by, created_at)
VALUES (@id, @version, @instructions, @by, @at)`, pgx.StrictNamedArgs{
		"id": a.ID, "version": a.InstructionsVersion, "instructions": a.Instructions, "by": a.UpdatedBy, "at": a.UpdatedAt,
	}).Fn = postgres.ExecAs("insert agent version", nil)
}

func (s pgSettings) CreateAgent(ctx context.Context, a Agent, e audit.Entry) error {
	b := &pgx.Batch{}
	b.Queue(`INSERT INTO ai_agents (id, agent_kind, display_name, slug, icon, purpose, required_tags, model_provider,
    model_id, fallbacks, extra_body, instructions_version, capabilities, limits, monthly_budget_usd, enabled, updated_by,
    updated_at, write_id)
VALUES (@id, @kind, @name, @slug, @icon, @purpose, @required, @provider, @model, @fallbacks, @extra, @version, @capabilities,
    @limits, @budget, @enabled, @by, @at, @write_id)`, agentArgs(a)).Fn = taken("insert ai agent")
	b.Queue(`INSERT INTO ai_agent_callers (caller_name, agent_id, requests_per_min, tokens_per_min)
SELECT c.name, @id, c.requests_per_min, c.tokens_per_min
FROM jsonb_to_recordset(@callers::jsonb) AS c(name text, requests_per_min int, tokens_per_min int)`,
		pgx.StrictNamedArgs{"id": a.ID, "callers": callersOf(a)}).Fn = taken("insert agent callers")
	queueVersion(b, a)
	audit.Queue(b, e)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

// callersOf is a's callers, [] when it has none.
func callersOf(a Agent) []Caller {
	if a.Callers == nil {
		return []Caller{}
	}
	return a.Callers
}

func (s pgSettings) SaveAgent(ctx context.Context, a Agent, readWriteID uuid.UUID, newVersion bool, e audit.Entry) error {
	args := agentArgs(a)
	delete(args, "kind")
	delete(args, "slug")
	args["read_write_id"] = readWriteID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		b := &pgx.Batch{}
		b.Queue(`UPDATE ai_agents SET display_name = @name, icon = @icon, purpose = @purpose,
    required_tags = @required, model_provider = @provider, model_id = @model, fallbacks = @fallbacks,
    extra_body = @extra, instructions_version = @version, capabilities = @capabilities, limits = @limits,
    monthly_budget_usd = @budget, enabled = @enabled, updated_by = @by, updated_at = @at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`, args).Fn = func(br pgx.BatchResults) error {
			tag, err := br.Exec()
			if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
				return ErrTaken
			}
			if err != nil {
				return fmt.Errorf("update ai agent: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return ErrConflict
			}
			return nil
		}
		b.Queue(`UPDATE ai_agent_callers AS a SET requests_per_min = c.requests_per_min, tokens_per_min = c.tokens_per_min
FROM jsonb_to_recordset(@callers::jsonb) AS c(name text, requests_per_min int, tokens_per_min int)
WHERE a.caller_name = c.name AND a.agent_id = @id`, pgx.StrictNamedArgs{"id": a.ID, "callers": callersOf(a)}).Fn = postgres.ExecAs("update agent callers", nil)
		if newVersion {
			queueVersion(b, a)
		}
		audit.Queue(b, e)
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("save agent: %w", err)
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

func (s pgSettings) Exclusive(ctx context.Context, fn func(context.Context) error) error {
	if err := postgres.WithLock(ctx, s.db, "ai_gateway", fn); err != nil {
		return fmt.Errorf("hold the ai_gateway lock: %w", err)
	}
	return nil
}
