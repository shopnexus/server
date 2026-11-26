package service

import (
	"bytes"
	"encoding/json"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
)

// The AI settings views are the schemas in api/schemas/ai_settings.yaml.

// Provider is the AIProvider schema: a provider of Bifrost's config.json,
// with its last hour of requests.
type Provider struct {
	Name string `json:"name"`
	// Kind is a built-in provider's name, or custom.
	Kind     string `json:"kind"`
	BaseHost string `json:"base_host,omitempty"`
	Status   string `json:"status"`
	// ErrorShare1h is the share of the last hour's requests that failed,
	// 0 to 1.
	ErrorShare1h float64 `json:"error_share_1h"`
	Requests1h   int     `json:"requests_1h"`
	ModelCount   int     `json:"model_count"`
}

// The statuses of a provider.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusDown     = "down"
	StatusUnknown  = "unknown"
)

// Price is the AIPrice schema, in USD per token; a nil cache price is the
// input price.
type Price struct {
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

// CatalogModel is the AIProviderModel schema: a model of a provider's
// catalog in Bifrost, with its list price.
type CatalogModel struct {
	Model          string `json:"model_id"`
	CatalogPrice   *Price `json:"catalog_price,omitempty"`
	MaxInputTokens *int   `json:"max_input_tokens,omitempty"`
}

// Tag is the AITag schema.
type Tag struct {
	Name string         `json:"name"`
	Kind domain.TagKind `json:"kind"`
}

// Model is the AIModel schema: a model an agent uses, or that has a price
// or tags, with its catalog's price beside its own.
type Model struct {
	Provider     string     `json:"provider"`
	Model        string     `json:"model_id"`
	Tags         []string   `json:"tags"`
	Price        *Price     `json:"price"`
	CatalogPrice *Price     `json:"catalog_price"`
	Agents       []AgentRef `json:"agents"`
}

// AgentRef is an agent in a list of another thing.
type AgentRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ModelList is the AIModelList schema, with the tags an admin may set.
type ModelList struct {
	Items []Model `json:"items"`
	Tags  []Tag   `json:"tags"`
}

// ModelTags is the AIModelTags schema: a model's tags, as a request and
// as its answer.
type ModelTags struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model_id"`
	Tags     []string `json:"tags"`
}

// ModelPrice is the AIModelPrice schema: a model's price, as a request and
// as its answer.
type ModelPrice struct {
	Price

	Provider string `json:"provider"`
	Model    string `json:"model_id"`
}

// Caller is the AIAgentCaller schema: one virtual key of an agent, with
// its rate limits per minute, nil for none.
type Caller struct {
	Name           string `json:"name"`
	RequestsPerMin *int   `json:"requests_per_min"`
	TokensPerMin   *int   `json:"tokens_per_min"`
}

// AgentView is the AIAgent schema.
type AgentView struct {
	ID                  uuid.UUID         `json:"id"`
	Kind                domain.AgentKind  `json:"kind"`
	Name                string            `json:"name"`
	Slug                string            `json:"slug"`
	Icon                string            `json:"icon"`
	Purpose             string            `json:"purpose"`
	RequiredTags        []string          `json:"required_tags"`
	Model               domain.ModelRef   `json:"model"`
	Fallbacks           []domain.ModelRef `json:"fallbacks"`
	Instructions        string            `json:"instructions,omitempty"`
	InstructionsVersion int               `json:"instructions_version"`
	Tools               []string          `json:"tools"`
	Limits              domain.Limits     `json:"limits"`
	MonthlyBudgetUSD    *float64          `json:"monthly_budget_usd"`
	// BudgetSpentUSD is this month's spend against the budget, and
	// BudgetResetsAt the 1st of next month (UTC), when it starts again.
	BudgetSpentUSD *float64   `json:"budget_spent_usd"`
	BudgetResetsAt *time.Time `json:"budget_resets_at"`
	Callers        []Caller   `json:"callers"`
	Enabled        bool       `json:"enabled"`
	UpdatedBy      *uuid.UUID `json:"updated_by,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// NewAgent is the AIAgentNew schema: a console agent, whose one caller is
// its slug.
type NewAgent struct {
	Name             string            `json:"name"`
	Slug             string            `json:"slug"`
	Icon             string            `json:"icon"`
	Purpose          string            `json:"purpose"`
	RequiredTags     []string          `json:"required_tags"`
	Model            domain.ModelRef   `json:"model"`
	Fallbacks        []domain.ModelRef `json:"fallbacks"`
	Instructions     string            `json:"instructions"`
	Tools            []string          `json:"tools"`
	Limits           domain.Limits     `json:"limits"`
	MonthlyBudgetUSD *float64          `json:"monthly_budget_usd,omitempty"`
	Enabled          *bool             `json:"enabled,omitempty"`
}

// AgentPatch is the AIAgentPatch schema; a nil field stays. An app agent
// takes Model, Fallbacks, Limits, MonthlyBudgetUSD and Callers only.
type AgentPatch struct {
	Name         *string            `json:"name,omitempty"`
	Icon         *string            `json:"icon,omitempty"`
	Purpose      *string            `json:"purpose,omitempty"`
	RequiredTags *[]string          `json:"required_tags,omitempty"`
	Model        *domain.ModelRef   `json:"model,omitempty"`
	Fallbacks    *[]domain.ModelRef `json:"fallbacks,omitempty"`
	Instructions *string            `json:"instructions,omitempty"`
	Tools        *[]string          `json:"tools,omitempty"`
	Limits       *domain.Limits     `json:"limits,omitempty"`
	// MonthlyBudgetUSD is absent, null for none, or an amount.
	MonthlyBudgetUSD Budget `json:"monthly_budget_usd,omitzero"`
	// Callers, when set, names every caller of the agent with its new
	// rate limits.
	Callers *[]Caller `json:"callers,omitempty"`
	Enabled *bool     `json:"enabled,omitempty"`
}

// Budget is a budget in USD in a patch: absent, null for none, or an
// amount.
type Budget struct {
	Set   bool
	Value *float64
}

func (b *Budget) UnmarshalJSON(raw []byte) error {
	b.Set = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		b.Value = nil
		return nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	b.Value = &v
	return nil
}

func (b Budget) IsZero() bool { return !b.Set }

func (b Budget) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.Value)
}

// AgentVersion is the AIAgentVersion schema.
type AgentVersion struct {
	Version      int        `json:"version"`
	Instructions string     `json:"instructions"`
	CreatedBy    *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// Spend is the AISpend schema: the spend of each day from From to To, in
// all and by agent slug, as Bifrost logged it.
type Spend struct {
	Days []SpendDay `json:"days"`
	// LagS is how far Bifrost's totals may trail real time.
	LagS int `json:"lag_s"`
}

// SpendDay is one UTC day's spend.
type SpendDay struct {
	Date     string             `json:"date"`
	TotalUSD float64            `json:"total_usd"`
	ByAgent  map[string]float64 `json:"by_agent"`
}

// ConsoleAgent is the ConsoleAgent schema: a console agent a member may
// pick in the ask box.
type ConsoleAgent struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Icon    string    `json:"icon"`
	Purpose string    `json:"purpose"`
	Default bool      `json:"default"`
}
