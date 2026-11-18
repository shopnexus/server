package service

import (
	"bytes"
	"encoding/json"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
)

// The AI settings views are the schemas in api/schemas/ai_settings.yaml.

// Provider is the AIProvider schema: a LiteLLM credential with what the
// server writes beside it.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"base_url,omitempty"`
	// KeyHint is the key as LiteLLM shows it, masked.
	KeyHint  string              `json:"key_hint,omitempty"`
	Weight   float64             `json:"weight"`
	Order    int                 `json:"order"`
	Mode     domain.ProviderMode `json:"mode"`
	Models   []string            `json:"models"`
	SyncedAt *time.Time          `json:"synced_at,omitempty"`
}

// NewProvider is the AIProviderNew schema.
type NewProvider struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	Kind    string              `json:"kind"`
	BaseURL string              `json:"base_url,omitempty"`
	APIKey  string              `json:"api_key"`
	Weight  *float64            `json:"weight,omitempty"`
	Order   *int                `json:"order,omitempty"`
	Mode    domain.ProviderMode `json:"mode,omitempty"`
}

// ProviderPatch is the AIProviderPatch schema; a nil field stays.
type ProviderPatch struct {
	Name    *string              `json:"name,omitempty"`
	BaseURL *string              `json:"base_url,omitempty"`
	APIKey  *string              `json:"api_key,omitempty"`
	Weight  *float64             `json:"weight,omitempty"`
	Order   *int                 `json:"order,omitempty"`
	Mode    *domain.ProviderMode `json:"mode,omitempty"`
}

// ProviderTest is the AIProviderTest schema: one call through LiteLLM with
// the provider's key.
type ProviderTest struct {
	OK        bool   `json:"ok"`
	Model     string `json:"model,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMS int    `json:"latency_ms"`
}

// Tag is the AITag schema.
type Tag struct {
	Name string         `json:"name"`
	Kind domain.TagKind `json:"kind"`
}

// Model is the AIModel schema: a model some provider serves, its tags,
// its price per million tokens and the agents that run on it.
type Model struct {
	Model             string     `json:"model"`
	Tags              []string   `json:"tags"`
	Providers         []Named    `json:"providers"`
	Agents            []AgentRef `json:"agents"`
	InputCostPerMTok  *float64   `json:"input_cost_per_mtok,omitempty"`
	OutputCostPerMTok *float64   `json:"output_cost_per_mtok,omitempty"`
}

// Named is a provider in a model's list.
type Named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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

// AgentView is the AIAgent schema.
type AgentView struct {
	ID                  uuid.UUID        `json:"id"`
	Kind                domain.AgentKind `json:"kind"`
	Name                string           `json:"name"`
	Alias               string           `json:"alias"`
	Icon                string           `json:"icon"`
	Purpose             string           `json:"purpose"`
	RequiredTags        []string         `json:"required_tags"`
	Model               string           `json:"model"`
	Instructions        string           `json:"instructions,omitempty"`
	InstructionsVersion int              `json:"instructions_version"`
	Tools               []string         `json:"tools"`
	Limits              domain.Limits    `json:"limits"`
	MonthlyBudgetUSD    *float64         `json:"monthly_budget_usd,omitempty"`
	Enabled             bool             `json:"enabled"`
	UpdatedBy           *uuid.UUID       `json:"updated_by,omitempty"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

// NewAgent is the AIAgentNew schema: a console agent.
type NewAgent struct {
	Name             string        `json:"name"`
	Alias            string        `json:"alias"`
	Icon             string        `json:"icon"`
	Purpose          string        `json:"purpose"`
	RequiredTags     []string      `json:"required_tags"`
	Model            string        `json:"model"`
	Instructions     string        `json:"instructions"`
	Tools            []string      `json:"tools"`
	Limits           domain.Limits `json:"limits"`
	MonthlyBudgetUSD *float64      `json:"monthly_budget_usd,omitempty"`
	Enabled          *bool         `json:"enabled,omitempty"`
}

// AgentPatch is the AIAgentPatch schema; a nil field stays. An app agent
// takes Model, Limits and MonthlyBudgetUSD only.
type AgentPatch struct {
	Name             *string        `json:"name,omitempty"`
	Icon             *string        `json:"icon,omitempty"`
	Purpose          *string        `json:"purpose,omitempty"`
	RequiredTags     *[]string      `json:"required_tags,omitempty"`
	Model            *string        `json:"model,omitempty"`
	Instructions     *string        `json:"instructions,omitempty"`
	Tools            *[]string      `json:"tools,omitempty"`
	Limits           *domain.Limits `json:"limits,omitempty"`
	MonthlyBudgetUSD Budget         `json:"monthly_budget_usd,omitzero"`
	Enabled          *bool          `json:"enabled,omitempty"`
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

// Rotation is the AIRotation schema: LiteLLM's router settings an admin
// sets. Providers' weights and order sit on the providers.
type Rotation struct {
	RoutingStrategy string  `json:"routing_strategy"`
	NumRetries      int     `json:"num_retries"`
	AllowedFails    int     `json:"allowed_fails"`
	CooldownS       float64 `json:"cooldown_s"`
	TimeoutS        float64 `json:"timeout_s"`
}

// RotationPatch is the AIRotationPatch schema.
type RotationPatch struct {
	RoutingStrategy *string  `json:"routing_strategy,omitempty"`
	NumRetries      *int     `json:"num_retries,omitempty"`
	AllowedFails    *int     `json:"allowed_fails,omitempty"`
	CooldownS       *float64 `json:"cooldown_s,omitempty"`
	TimeoutS        *float64 `json:"timeout_s,omitempty"`
}

// Budgets is the AIBudgets schema: each agent's monthly budget and what it
// spent of it, and the daily budget of each member.
type Budgets struct {
	Agents         []AgentBudget `json:"agents"`
	MemberDailyUSD *float64      `json:"member_daily_usd,omitempty"`
}

// AgentBudget is one agent's budget and spend this period.
type AgentBudget struct {
	AgentID          uuid.UUID        `json:"agent_id"`
	Name             string           `json:"name"`
	Kind             domain.AgentKind `json:"kind"`
	MonthlyBudgetUSD *float64         `json:"monthly_budget_usd,omitempty"`
	SpentUSD         float64          `json:"spent_usd"`
}

// BudgetsPatch is the AIBudgetsPatch schema.
type BudgetsPatch struct {
	Agents         []AgentBudgetPatch `json:"agents,omitempty"`
	MemberDailyUSD Budget             `json:"member_daily_usd,omitzero"`
}

// AgentBudgetPatch sets one agent's monthly budget, null for none.
type AgentBudgetPatch struct {
	AgentID          uuid.UUID `json:"agent_id"`
	MonthlyBudgetUSD Budget    `json:"monthly_budget_usd"`
}

// Spend is the AISpend schema: the spend of each day of a range, in all
// and by agent.
type Spend struct {
	Range    string     `json:"range"`
	Start    string     `json:"start"`
	End      string     `json:"end"`
	TotalUSD float64    `json:"total_usd"`
	Days     []SpendDay `json:"days"`
}

// SpendDay is one day's spend.
type SpendDay struct {
	Date     string       `json:"date"`
	TotalUSD float64      `json:"total_usd"`
	Agents   []AgentSpend `json:"agents"`
}

// AgentSpend is an agent's spend on a day.
type AgentSpend struct {
	AgentID uuid.UUID `json:"agent_id"`
	USD     float64   `json:"usd"`
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
