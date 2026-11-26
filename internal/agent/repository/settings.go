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
	// ErrTaken is another agent's name or slug, or another's caller.
	ErrTaken = errors.New("agent repository: name, slug or caller taken")
	// ErrUnknownTag is a model tag ai_tags lacks.
	ErrUnknownTag = errors.New("agent repository: unknown tag")
)

// Tag is a row of ai_tags.
type Tag struct {
	Name string
	Kind domain.TagKind
	Sort int
}

// Agent is a row of ai_agents with its latest version's instructions and
// its callers.
type Agent struct {
	ID           uuid.UUID
	Kind         domain.AgentKind
	Name         string
	Slug         string
	Icon         string
	Purpose      string
	RequiredTags []string
	Model        domain.ModelRef
	Fallbacks    []domain.ModelRef
	// ExtraBody goes into each request to the model as it is.
	ExtraBody           map[string]any
	Instructions        string
	InstructionsVersion int
	Tools               []string
	Limits              domain.Limits
	MonthlyBudgetUSD    *float64
	// Callers are sorted by name.
	Callers   []Caller
	Enabled   bool
	UpdatedBy *uuid.UUID
	UpdatedAt time.Time
	WriteID   uuid.UUID
}

// Caller is a row of ai_agent_callers: one virtual key of an agent, with
// its rate limits per minute, nil for none.
type Caller struct {
	Name           string `json:"name"`
	RequestsPerMin *int   `json:"requests_per_min"`
	TokensPerMin   *int   `json:"tokens_per_min"`
}

// Price is a row of ai_model_prices, in USD per token; a nil cache price
// is the input price.
type Price struct {
	Model      domain.ModelRef
	Input      float64
	Output     float64
	CacheRead  *float64
	CacheWrite *float64
	UpdatedBy  *uuid.UUID
	UpdatedAt  time.Time
}

// Version is one version of a console agent's instructions.
type Version struct {
	Version      int
	Instructions string
	CreatedBy    *uuid.UUID
	CreatedAt    time.Time
}

// Settings keeps the AI policy: the tags, the models' tags and prices, and
// the agents with their callers and their instructions' versions. A write
// records its console action in the same transaction.
type Settings interface { //nolint:interfacebloat // module agent's one store of the AI settings, as a repository.go store.
	// Tags lists ai_tags in their order.
	Tags(ctx context.Context) ([]Tag, error)
	// ModelTags maps each model that has tags to them, in the tags' order.
	ModelTags(ctx context.Context) (map[domain.ModelRef][]string, error)
	// SetModelTags replaces model's tags; a tag ai_tags lacks is
	// ErrUnknownTag and changes nothing.
	SetModelTags(ctx context.Context, model domain.ModelRef, tags []string, e audit.Entry) error
	// Prices lists every model's price.
	Prices(ctx context.Context) ([]Price, error)
	// SetPrice writes a model's price, a new row or over the old one.
	SetPrice(ctx context.Context, p Price, e audit.Entry) error
	// Agents lists every agent, app agents first, by name.
	Agents(ctx context.Context) ([]Agent, error)
	// Agent reads one agent, or ErrNotFound.
	Agent(ctx context.Context, id uuid.UUID) (Agent, error)
	// CreateAgent inserts a with its callers and its first version; another
	// agent's name or slug, or another's caller, is ErrTaken.
	CreateAgent(ctx context.Context, a Agent, e audit.Entry) error
	// SaveAgent writes a over the row read with readWriteID, which a's
	// WriteID replaces, with its callers' rate limits, and with newVersion
	// adds a's instructions as version a.InstructionsVersion, which must not
	// exist. ErrConflict when the row changed, ErrTaken on another agent's
	// name.
	SaveAgent(ctx context.Context, a Agent, readWriteID uuid.UUID, newVersion bool, e audit.Entry) error
	// Versions lists an agent's versions, newest first.
	Versions(ctx context.Context, id uuid.UUID) ([]Version, error)
	// Exclusive runs fn while no other process runs one of its own, as
	// the reconciler does.
	Exclusive(ctx context.Context, fn func(context.Context) error) error
}
