package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/platform/audit"
)

var (
	// ErrNotFound is an agent that does not exist.
	ErrNotFound = errors.New("agent repository: not found")
	// ErrConflict is a save of a row that changed since it was read.
	ErrConflict = errors.New("agent repository: conflict")
	// ErrTaken is another agent's name or alias.
	ErrTaken = errors.New("agent repository: name or alias taken")
	// ErrUnknownTag is a model tag ai_tags lacks.
	ErrUnknownTag = errors.New("agent repository: unknown tag")
)

// Tag is a row of ai_tags.
type Tag struct {
	Name string
	Kind domain.TagKind
	Sort int
}

// Agent is a row of ai_agents.
type Agent struct {
	ID                  uuid.UUID
	Kind                domain.AgentKind
	Name                string
	Alias               string
	Icon                string
	Purpose             string
	RequiredTags        []string
	Model               string
	Instructions        string
	InstructionsVersion int
	Tools               []string
	Limits              domain.Limits
	MonthlyBudgetUSD    *float64
	KeyAliases          []string
	Enabled             bool
	UpdatedBy           *uuid.UUID
	UpdatedAt           time.Time
	WriteID             uuid.UUID
}

// Version is one version of a console agent's instructions.
type Version struct {
	Version      int
	Instructions string
	CreatedBy    *uuid.UUID
	CreatedAt    time.Time
}

// Settings keeps the AI settings LiteLLM has no place for: the tags, the
// models' tags and the agents with their instructions' versions. A write
// records its console action in the same transaction.
type Settings interface { //nolint:interfacebloat // module agent's one store of the AI settings, as a repository.go store.
	// Tags lists ai_tags in their order.
	Tags(ctx context.Context) ([]Tag, error)
	// ModelTags maps each model that has tags to them, in the tags' order.
	ModelTags(ctx context.Context) (map[string][]string, error)
	// SetModelTags replaces model's tags; a tag ai_tags lacks is
	// ErrUnknownTag and changes nothing.
	SetModelTags(ctx context.Context, model string, tags []string, e audit.Entry) error
	// Agents lists every agent, app agents first, by name.
	Agents(ctx context.Context) ([]Agent, error)
	// Agent reads one agent, or ErrNotFound.
	Agent(ctx context.Context, id uuid.UUID) (Agent, error)
	// CreateAgent inserts a and its first version; another agent's name or
	// alias is ErrTaken.
	CreateAgent(ctx context.Context, a Agent, e audit.Entry) error
	// SaveAgent writes a over the row read with readWriteID, which a's
	// WriteID replaces, and adds a version when a's instructions version
	// is new. ErrConflict when the row changed, ErrTaken on another
	// agent's name.
	SaveAgent(ctx context.Context, a Agent, readWriteID uuid.UUID, e audit.Entry) error
	// Versions lists an agent's versions, newest first.
	Versions(ctx context.Context, id uuid.UUID) ([]Version, error)
	// Record writes a console action on its own, for a change made in
	// LiteLLM.
	Record(ctx context.Context, e audit.Entry) error
	// BudgetSettings reads the one row of ai_budget_settings and the
	// members' own daily budgets.
	BudgetSettings(ctx context.Context) (BudgetSettings, error)
	// SaveBudgetSettings writes b over the row read with readWriteID,
	// which b's WriteID replaces, and replaces the members' own budgets with
	// b.Members when b.SetMembers, with e in the same transaction.
	// ErrConflict when the row changed, ErrNotFound for an unknown member.
	SaveBudgetSettings(ctx context.Context, b BudgetSettings, readWriteID uuid.UUID, e audit.Entry) error
}

// BudgetSettings is the AI spend's monthly cap, its alert and what the cap
// does, and the console agents' daily budget per member: one per role and
// the members' own.
type BudgetSettings struct {
	MonthlyCapUSD     *float64
	AlertPct          int
	OnCap             string
	ModeratorDailyUSD *float64
	AdminDailyUSD     *float64
	Members           []MemberBudget
	// SetMembers says that Members replaces the stored list.
	SetMembers bool
	UpdatedBy  *uuid.UUID
	UpdatedAt  time.Time
	WriteID    uuid.UUID
}

// MemberBudget is a member's own daily budget.
type MemberBudget struct {
	MemberID uuid.UUID
	DailyUSD float64
}
